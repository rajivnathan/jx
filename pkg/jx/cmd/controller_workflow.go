package cmd

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/jenkins-x/jx/pkg/apis/jenkins.io/v1"
	"github.com/jenkins-x/jx/pkg/client/clientset/versioned"
	typev1 "github.com/jenkins-x/jx/pkg/client/clientset/versioned/typed/jenkins.io/v1"
	"github.com/jenkins-x/jx/pkg/gits"
	"github.com/jenkins-x/jx/pkg/helm"
	"github.com/jenkins-x/jx/pkg/log"
	"github.com/jenkins-x/jx/pkg/util"
	"github.com/jenkins-x/jx/pkg/workflow"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/cache"

	"github.com/jenkins-x/jx/pkg/kube"
)

// ControllerWorkflowOptions are the flags for the commands
type ControllerWorkflowOptions struct {
	ControllerOptions

	Namespace           string
	NoWatch             bool
	NoMergePullRequest  bool
	Verbose             bool
	LocalHelmRepoName   string
	PullRequestPollTime string

	// testing
	FakePullRequests CreateEnvPullRequestFn
	FakeGitProvider  *gits.FakeProvider

	// calculated fields
	PullRequestPollDuration *time.Duration
	workflowMap             map[string]*v1.Workflow
	pipelineMap             map[string]*v1.PipelineActivity
}

// NewCmdControllerWorkflow creates a command object for the generic "get" action, which
// retrieves one or more resources from a server.
func NewCmdControllerWorkflow(f Factory, out io.Writer, errOut io.Writer) *cobra.Command {
	options := &ControllerWorkflowOptions{
		ControllerOptions: ControllerOptions{
			CommonOptions: CommonOptions{
				Factory: f,
				Out:     out,
				Err:     errOut,
			},
		},
	}

	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "Runs the workflow controller",
		Run: func(cmd *cobra.Command, args []string) {
			options.Cmd = cmd
			options.Args = args
			err := options.Run()
			CheckErr(err)
		},
		Aliases: []string{"workflows"},
	}

	cmd.Flags().StringVarP(&options.Namespace, "namespace", "n", "", "The namespace to watch or defaults to the current namespace")
	cmd.Flags().StringVarP(&options.LocalHelmRepoName, "helm-repo-name", "r", kube.LocalHelmRepoName, "The name of the helm repository that contains the app")
	cmd.Flags().BoolVarP(&options.NoWatch, "no-watch", "", false, "Disable watch so just performs any delta processes on pending workflows")
	cmd.Flags().StringVarP(&options.PullRequestPollTime, optionPullRequestPollTime, "", "20s", "Poll time when waiting for a Pull Request to merge")
	return cmd
}

// Run implements this command
func (o *ControllerWorkflowOptions) Run() error {
	err := o.registerWorkflowCRD()
	if err != nil {
		return err
	}
	err = o.registerWorkflowCRD()
	if err != nil {
		return err
	}

	if o.PullRequestPollTime != "" {
		duration, err := time.ParseDuration(o.PullRequestPollTime)
		if err != nil {
			return fmt.Errorf("Invalid duration format %s for option --%s: %s", o.PullRequestPollTime, optionPullRequestPollTime, err)
		}
		o.PullRequestPollDuration = &duration
	}

	jxClient, devNs, err := o.JXClientAndDevNamespace()
	if err != nil {
		return err
	}

	ns := o.Namespace
	if ns == "" {
		ns = devNs
	}

	o.workflowMap = map[string]*v1.Workflow{}
	o.pipelineMap = map[string]*v1.PipelineActivity{}

	if o.NoWatch {
		return o.updatePipelinesWithoutWatching(jxClient, ns)
	}

	log.Infof("Watching for PipelineActivity resources in namespace %s\n", util.ColorInfo(ns))
	workflow := &v1.Workflow{}
	activity := &v1.PipelineActivity{}
	workflowListWatch := cache.NewListWatchFromClient(jxClient.JenkinsV1().RESTClient(), "workflows", ns, fields.Everything())
	kube.SortListWatchByName(workflowListWatch)
	_, workflowController := cache.NewInformer(
		workflowListWatch,
		workflow,
		time.Minute*10,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				o.onWorkflowObj(obj, jxClient, ns)
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				o.onWorkflowObj(newObj, jxClient, ns)
			},
			DeleteFunc: func(obj interface{}) {
				o.deleteWorkflowObjb(obj, jxClient, ns)
			},
		},
	)
	stop := make(chan struct{})
	go workflowController.Run(stop)

	pipelineListWatch := cache.NewListWatchFromClient(jxClient.JenkinsV1().RESTClient(), "pipelineactivities", ns, fields.Everything())
	kube.SortListWatchByName(pipelineListWatch)
	_, pipelineController := cache.NewInformer(
		pipelineListWatch,
		activity,
		time.Minute*10,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				o.onActivityObj(obj, jxClient, ns)
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				o.onActivityObj(newObj, jxClient, ns)
			},
			DeleteFunc: func(obj interface{}) {
			},
		},
	)

	go pipelineController.Run(stop)

	ticker := time.NewTicker(*o.PullRequestPollDuration)
	go func() {
		for t := range ticker.C {
			if o.Verbose {
				log.Infof("Polling to see if any PRs have merged: %v\n", t)
			}
			o.checkPullRequests(jxClient, ns)
		}
	}()

	// Wait forever
	select {}
}

func (o *ControllerWorkflowOptions) updatePipelinesWithoutWatching(jxClient versioned.Interface, ns string) error {
	workflows, err := jxClient.JenkinsV1().Workflows(ns).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, workflow := range workflows.Items {
		o.onWorkflow(&workflow, jxClient, ns)
	}

	pipelines, err := jxClient.JenkinsV1().PipelineActivities(ns).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, pipeline := range pipelines.Items {
		o.onActivity(&pipeline, jxClient, ns)
	}
	return nil
}

func (o *ControllerWorkflowOptions) onWorkflowObj(obj interface{}, jxClient versioned.Interface, ns string) {
	workflow, ok := obj.(*v1.Workflow)
	if !ok {
		log.Warnf("Object is not a Workflow %#v\n", obj)
		return
	}
	if workflow != nil {
		o.onWorkflow(workflow, jxClient, ns)
	}
}

func (o *ControllerWorkflowOptions) deleteWorkflowObjb(obj interface{}, jxClient versioned.Interface, ns string) {
	workflow, ok := obj.(*v1.Workflow)
	if !ok {
		log.Warnf("Object is not a Workflow %#v\n", obj)
		return
	}
	if workflow != nil {
		o.onWorkflowDelete(workflow, jxClient, ns)
	}
}

func (o *ControllerWorkflowOptions) onWorkflow(workflow *v1.Workflow, jxClient versioned.Interface, ns string) {
	o.workflowMap[workflow.Name] = workflow
}

func (o *ControllerWorkflowOptions) onWorkflowDelete(workflow *v1.Workflow, jxClient versioned.Interface, ns string) {
	delete(o.workflowMap, workflow.Name)
}

func (o *ControllerWorkflowOptions) onActivityObj(obj interface{}, jxClient versioned.Interface, ns string) {
	pipeline, ok := obj.(*v1.PipelineActivity)
	if !ok {
		log.Warnf("Object is not a PipelineActivity %#v\n", obj)
		return
	}
	if pipeline != nil {
		o.onActivity(pipeline, jxClient, ns)
	}
}

func (o *ControllerWorkflowOptions) onActivity(pipeline *v1.PipelineActivity, jxClient versioned.Interface, ns string) {
	workflowName := pipeline.Spec.Workflow
	version := pipeline.Spec.Version
	repoName := pipeline.RepositoryName()
	branch := pipeline.BranchName()
	pipelineName := pipeline.Spec.Pipeline
	build := pipeline.Spec.Build

	if o.Verbose {
		log.Infof("Processing pipeline %s repo %s version %s with workflow %s and status %s\n", pipeline.Name, repoName, version, workflowName, string(pipeline.Spec.WorkflowStatus))
	}

	if workflowName == "" {
		workflowName = "default"
	}
	if repoName == "" || version == "" || build == "" || pipelineName == "" {
		if o.Verbose {
			log.Infof("Ignoring missing data for pipeline: %s repo: %s version: %s status: %s\n", pipeline.Name, repoName, version, string(pipeline.Spec.WorkflowStatus))
		}
		o.removePipelineActivity(pipeline)
		return
	}
	if !pipeline.Spec.WorkflowStatus.IsTerminated() {
		flow := o.workflowMap[workflowName]
		if flow == nil && workflowName == "default" {
			var err error
			flow, err = workflow.CreateDefaultWorkflow(jxClient, ns)
			if err != nil {
				log.Warnf("Cannot create default Workflow: %s\n", err)
				o.removePipelineActivity(pipeline)
				return
			}

		}

		if flow == nil {
			log.Warnf("Cannot process pipeline %s due to workflow name %s not existing\n", pipeline.Name, workflowName)
			o.removePipelineActivity(pipeline)
			return
		}

		if !o.isReleaseBranch(branch) {
			log.Infof("Ignoring branch %s\n", branch)
			o.removePipelineActivity(pipeline)
			return
		}

		// ensure the pipeline is in our map
		o.pipelineMap[pipeline.Name] = pipeline

		// lets walk the Workflow spec and see if we need to trigger any PRs or move the PipelineActivity forward
		promoteStatusMap := createPromoteStatus(pipeline)

		for _, step := range flow.Spec.Steps {
			promote := step.Promote
			if promote != nil {
				envName := promote.Environment
				if envName != "" {
					status := promoteStatusMap[envName]
					if status == nil || status.PullRequest == nil || status.PullRequest.PullRequestURL == "" {
						// can we generate a PR now?
						if canExecuteStep(flow, pipeline, &step, promoteStatusMap, envName) {
							log.Infof("Creating PR for environment %s from PipelineActivity %s\n", envName, pipeline.Name)
							po := o.createPromoteOptions(repoName, envName, pipelineName, build, version)

							err := po.Run()
							if err != nil {
								log.Warnf("Failed to create PullRequest on pipeline %s repo %s version %s with workflow %s: %s\n", pipeline.Name, repoName, version, workflowName, err)
							}
						}
					}
				}
			}
		}
	}
}

func (o *ControllerWorkflowOptions) createPromoteOptions(repoName string, envName string, pipelineName string, build string, version string) *PromoteOptions {
	po := &PromoteOptions{
		Application:       repoName,
		Environment:       envName,
		Pipeline:          pipelineName,
		Build:             build,
		Version:           version,
		NoPoll:            true,
		IgnoreLocalFiles:  true,
		HelmRepositoryURL: helm.DefaultHelmRepositoryURL,
		LocalHelmRepoName: kube.LocalHelmRepoName,
		FakePullRequests:  o.FakePullRequests,
	}
	po.CommonOptions = o.CommonOptions
	po.BatchMode = true
	return po
}

func (o *ControllerWorkflowOptions) createPromoteOptionsFromActivity(pipeline *v1.PipelineActivity, envName string) *PromoteOptions {
	version := pipeline.Spec.Version
	repoName := pipeline.Spec.GitRepository
	pipelineName := pipeline.Spec.Pipeline
	build := pipeline.Spec.Build

	paths := strings.Split(pipelineName, "/")
	if repoName == "" && len(paths) > 1 {
		repoName = paths[len(paths)-2]
	}
	return o.createPromoteOptions(repoName, envName, pipelineName, build, version)
}

func (o *ControllerWorkflowOptions) createGitProviderForPR(prURL string) (gits.GitProvider, *gits.GitRepositoryInfo, error) {
	// lets remove the id
	idx := strings.LastIndex(prURL, "/")
	if idx <= 0 {
		return nil, nil, fmt.Errorf("No / in URL: %s", prURL)
	}
	gitUrl := prURL[0:idx]
	idx = strings.LastIndex(gitUrl, "/")
	if idx <= 0 {
		return nil, nil, fmt.Errorf("No / in URL: %s", gitUrl)
	}
	gitUrl = gitUrl[0:idx] + ".git"
	if o.FakeGitProvider != nil {
		gitInfo, err := gits.ParseGitURL(gitUrl)
		if err != nil {
			return nil, gitInfo, err
		}
		return o.FakeGitProvider, gitInfo, nil
	}
	answer, gitInfo, err := o.createGitProviderForURLWithoutKind(gitUrl)
	if err != nil {
		return answer, gitInfo, errors.Wrapf(err, "Failed for git URL %s", gitUrl)
	}
	return answer, gitInfo, nil
}

func (o *ControllerWorkflowOptions) createGitProvider(activity *v1.PipelineActivity) (gits.GitProvider, *gits.GitRepositoryInfo, error) {
	gitUrl := activity.Spec.GitURL
	if gitUrl == "" {
		return nil, nil, fmt.Errorf("No GitURL for PipelineActivity %s", activity.Name)
	}
	if o.FakeGitProvider != nil {
		gitInfo, err := gits.ParseGitURL(gitUrl)
		if err != nil {
			return nil, gitInfo, err
		}
		return o.FakeGitProvider, gitInfo, nil
	}
	answer, gitInfo, err := o.createGitProviderForURLWithoutKind(gitUrl)
	if err != nil {
		return answer, gitInfo, errors.Wrapf(err, "Failed for git URL %s", gitUrl)
	}
	return answer, gitInfo, nil
}

// checkPullRequests lets poll all the pending PipelineActivity resources to see if any of them
// have PR has merged or the pipeline on master has completed
func (o *ControllerWorkflowOptions) checkPullRequests(jxClient versioned.Interface, ns string) {
	environments := jxClient.JenkinsV1().Environments(ns)
	activities := jxClient.JenkinsV1().PipelineActivities(ns)

	for _, activity := range o.pipelineMap {
		o.checkPullRequest(activity, activities, environments, ns)
	}
}

// checkPullRequest polls the pending PipelineActivity resources to see if the
// PR has merged or the pipeline on master has completed
func (o *ControllerWorkflowOptions) checkPullRequest(activity *v1.PipelineActivity, activities typev1.PipelineActivityInterface, environments typev1.EnvironmentInterface, ns string) {
	if !o.isReleaseBranch(activity.BranchName()) {
		return
	}

	for _, step := range activity.Spec.Steps {
		promote := step.Promote
		if promote == nil || promote.Status.IsTerminated() {
			continue
		}
		envName := promote.Environment
		pullRequestStep := promote.PullRequest
		prURL := pullRequestStep.PullRequestURL
		if pullRequestStep == nil || prURL == "" || envName == "" {
			continue
		}
		gitProvider, gitInfo, err := o.createGitProviderForPR(prURL)
		if err != nil {
			log.Warnf("Failed to create git Provider: %s", err)
			return
		}
		if gitProvider == nil || gitInfo == nil {
			return
		}
		prNumber, err := pullRequestURLToNumber(prURL)
		if err != nil {
			log.Warnf("Failed to get PR number: %s", err)
			return
		}
		pr, err := gitProvider.GetPullRequest(gitInfo.Organisation, gitInfo, prNumber)
		if err != nil {
			log.Warnf("Failed to query the Pull Request status on pipeline %s for repo %s PR %d for PR %s: %s", activity.Name, gitInfo.HttpsURL(), prNumber, prURL, err)
		} else {
			log.Infof("Pipeline %s promote Environment %s has PR %s\n", activity.Name, envName, prURL)

			po := o.createPromoteOptionsFromActivity(activity, envName)

			if pr.Merged != nil && *pr.Merged {
				if pr.MergeCommitSHA != nil {
					mergeSha := *pr.MergeCommitSHA
					mergedPR := func(a *v1.PipelineActivity, s *v1.PipelineActivityStep, ps *v1.PromoteActivityStep, p *v1.PromotePullRequestStep) error {
						kube.CompletePromotionPullRequest(a, s, ps, p)
						p.MergeCommitSHA = mergeSha
						return nil
					}
					env, err := environments.Get(envName, metav1.GetOptions{})
					if err != nil {
						log.Warnf("Failed to find environment %s: %s\n", envName, err)
						return
					} else {
						promoteKey := po.createPromoteKey(env)
						promoteKey.OnPromotePullRequest(activities, mergedPR)
						promoteKey.OnPromoteUpdate(activities, kube.StartPromotionUpdate)

						statuses, err := gitProvider.ListCommitStatus(pr.Owner, pr.Repo, mergeSha)
						if err == nil {
							urlStatusMap := map[string]string{}
							urlStatusTargetURLMap := map[string]string{}
							if len(statuses) > 0 {
								for _, status := range statuses {
									if status.IsFailed() {
										log.Warnf("merge status: %s URL: %s description: %s\n",
											status.State, status.TargetURL, status.Description)
										return
									}
									url := status.URL
									state := status.State
									if urlStatusMap[url] == "" || urlStatusMap[url] != gitStatusSuccess {
										if urlStatusMap[url] != state {
											urlStatusMap[url] = state
											urlStatusTargetURLMap[url] = status.TargetURL
										}
									}
								}
								prStatuses := []v1.GitStatus{}
								keys := util.SortedMapKeys(urlStatusMap)
								for _, url := range keys {
									state := urlStatusMap[url]
									targetURL := urlStatusTargetURLMap[url]
									if targetURL == "" {
										targetURL = url
									}
									prStatuses = append(prStatuses, v1.GitStatus{
										URL:    targetURL,
										Status: state,
									})
								}
								updateStatuses := func(a *v1.PipelineActivity, s *v1.PipelineActivityStep, ps *v1.PromoteActivityStep, p *v1.PromoteUpdateStep) error {
									p.Statuses = prStatuses
									return nil
								}
								promoteKey.OnPromoteUpdate(activities, updateStatuses)

								succeeded := true
								for _, v := range urlStatusMap {
									if v != gitStatusSuccess {
										succeeded = false
									}
								}
								if succeeded {
									gitURL := activity.Spec.GitURL
									if gitURL == "" {
										log.Warnf("No git URL for PipelineActivity %s so cannotcomment on issues\n", activity.Name)
										return
									}
									gitInfo, err := gits.ParseGitURL(gitURL)
									if err != nil {
										log.Warnf("Failed to parse git URL %s for PipelineActivity %s so cannot comment on issues: %s", gitURL, activity.Name, err)
										return
									}
									po.GitInfo = gitInfo
									err = po.commentOnIssues(ns, env, promoteKey)
									if err != nil {
										log.Warnf("Failed to comment on issues: %s", err)
										return
									}
									err = promoteKey.OnPromoteUpdate(activities, kube.CompletePromotionUpdate)
									if err != nil {
										log.Warnf("Failed to update PipelineActivity on promotion completion: %s", err)
									}
									return
								}
							}
						}
					}
				}
			} else {
				if pr.IsClosed() {
					log.Warnf("Pull Request %s is closed\n", util.ColorInfo(pr.URL))
					// TODO should we mark the PipelineActivity as complete?
					return
				}

				// lets try merge if the status is good
				status, err := gitProvider.PullRequestLastCommitStatus(pr)
				if err != nil {
					log.Warnf("Failed to query the Pull Request last commit status for %s ref %s %s\n", pr.URL, pr.LastCommitSha, err)
					//return fmt.Errorf("Failed to query the Pull Request last commit status for %s ref %s %s", pr.URL, pr.LastCommitSha, err)
				} else if status == "in-progress" {
					log.Infoln("The build for the Pull Request last commit is currently in progress.")
				} else {
					if status == "success" {
						if !o.NoMergePullRequest {
							err = gitProvider.MergePullRequest(pr, "jx promote automatically merged promotion PR")
							if err != nil {
								log.Warnf("Failed to merge the Pull Request %s due to %s maybe I don't have karma?\n", pr.URL, err)
							}
						}
					} else if status == "error" || status == "failure" {
						log.Warnf("Pull request %s last commit has status %s for ref %s", pr.URL, status, pr.LastCommitSha)
						return
					}
				}
			}
			if pr.Mergeable != nil && !*pr.Mergeable {
				log.Infoln("Rebasing PullRequest due to conflict")
				env, err := environments.Get(envName, metav1.GetOptions{})
				if err != nil {
					log.Warnf("Failed to find environment %s: %s\n", envName, err)
				} else {
					releaseInfo := o.createReleaseInfo(activity, env)
					if releaseInfo != nil {
						err = po.PromoteViaPullRequest(env, releaseInfo)
					}
				}
			}
		}
	}
}

func (o *ControllerWorkflowOptions) createReleaseInfo(activity *v1.PipelineActivity, env *v1.Environment) *ReleaseInfo {
	spec := &activity.Spec
	app := activity.RepositoryName()
	if app == "" {
		return nil
	}
	fullAppName := app
	if o.LocalHelmRepoName != "" {
		fullAppName = o.LocalHelmRepoName + "/" + app
	}
	releaseName := "" // TODO o.ReleaseName
	if releaseName == "" {
		releaseName = env.Spec.Namespace + "-" + app
		o.ReleaseName = releaseName
	}
	return &ReleaseInfo{
		ReleaseName: releaseName,
		FullAppName: fullAppName,
		Version:     spec.Version,
	}
}

func canExecuteStep(workflow *v1.Workflow, activity *v1.PipelineActivity, step *v1.WorkflowStep, statusMap map[string]*v1.PromoteActivityStep, promoteToEnv string) bool {
	for _, envName := range step.Preconditions.Environments {
		status := statusMap[envName]
		if status == nil {
			log.Warnf("Cannot promote to Environment: %s as precondition Environment: %s as no status\n", promoteToEnv, envName)
			return false
		}
		if status.Status != v1.ActivityStatusTypeSucceeded {
			log.Warnf("Cannot promote to Environment: %s as precondition Environment: %s has status %s", promoteToEnv, envName, string(status.Status))
			return false
		}
	}
	return true
}

// createPromoteStatus returns a map indexed by environment name of all the promotions in this pipeline
func createPromoteStatus(pipeline *v1.PipelineActivity) map[string]*v1.PromoteActivityStep {
	answer := map[string]*v1.PromoteActivityStep{}
	for _, step := range pipeline.Spec.Steps {
		promote := step.Promote
		if promote != nil {
			envName := promote.Environment
			if envName != "" {
				answer[envName] = promote
			}
		}
	}
	return answer
}

// createPromoteStepActivityKey deduces the pipeline metadata from the knative workflow pod
func (o *ControllerWorkflowOptions) createPromoteStepActivityKey(buildName string, pod *corev1.Pod) *kube.PromoteStepActivityKey {
	branch := ""
	lastCommitSha := ""
	lastCommitMessage := ""
	lastCommitURL := ""
	build := digitSuffix(buildName)
	if build == "" {
		build = "1"
	}
	gitUrl := ""
	for _, initContainer := range pod.Spec.InitContainers {
		if initContainer.Name == "workflow-step-git-source" {
			args := initContainer.Args
			for i := 0; i <= len(args)-2; i += 2 {
				key := args[i]
				value := args[i+1]

				switch key {
				case "-url":
					gitUrl = value
				case "-revision":
					branch = value
				}
			}
			break
		}
	}
	if gitUrl == "" {
		return nil
	}
	if branch == "" {
		branch = "master"
	}
	gitInfo, err := gits.ParseGitURL(gitUrl)
	if err != nil {
		log.Warnf("Failed to parse git URL %s: %s", gitUrl, err)
		return nil
	}
	org := gitInfo.Organisation
	repo := gitInfo.Name
	name := org + "-" + repo + "-" + branch + "-" + build
	pipeline := org + "/" + repo + "/" + branch
	return &kube.PromoteStepActivityKey{
		PipelineActivityKey: kube.PipelineActivityKey{
			Name:              name,
			Pipeline:          pipeline,
			Build:             build,
			LastCommitSHA:     lastCommitSha,
			LastCommitMessage: lastCommitMessage,
			LastCommitURL:     lastCommitURL,
			GitInfo:           gitInfo,
		},
	}
}

func pullRequestURLToNumber(text string) (int, error) {
	paths := strings.Split(strings.TrimSuffix(text, "/"), "/")
	lastPath := paths[len(paths)-1]
	prNumber, err := strconv.Atoi(lastPath)
	if err != nil {
		return 0, errors.Wrapf(err, "Failed to parse PR number from %s on URL %s", lastPath, text)
	}
	return prNumber, nil
}

func (o *ControllerWorkflowOptions) isReleaseBranch(branchName string) bool {
	// TODO look in TeamSettings for a list of allowed release branch patterns
	return branchName == "master"
}

func (o *ControllerWorkflowOptions) removePipelineActivity(activity *v1.PipelineActivity) {
	delete(o.pipelineMap, activity.Name)
}