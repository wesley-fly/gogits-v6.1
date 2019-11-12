package repo

import (
	"strings"

	git "github.com/gogits/git-module"
	api "github.com/gogits/go-gogs-client"
	"github.com/gogits/gogs/models"
	"github.com/gogits/gogs/pkg/context"
	"github.com/gogits/gogs/pkg/setting"
	"github.com/gogits/gogs/pkg/tool"
	log "gopkg.in/clog.v1"
)

func ParseCompareInfo(c *context.APIContext) (*models.User, *models.Repository, *git.Repository, *git.PullRequestInfo, string, string) {
	baseRepo := c.Repo.Repository

	// Get compared branches information
	// format: <base branch>...[<head repo>:]<head branch>
	// base<-head: master...head:feature
	// same repo: master...feature
	infos := strings.Split(c.Params(":compareInfo"), "...")
	if len(infos) != 2 {
		log.Trace("ParseCompareInfo[%d]: not enough compared branches information %s", baseRepo.ID, infos)
		return nil, nil, nil, nil, "", ""
	}

	baseBranch := infos[0]
	log.Trace("ParseCompareInfo - baseBranch is %s", baseBranch)

	var (
		headUser   *models.User
		headBranch string
		isSameRepo bool
		err        error
	)

	// If there is no head repository, it means pull request between same repository.
	headInfos := strings.Split(infos[1], ":")
	if len(headInfos) == 1 {
		isSameRepo = true
		headUser = c.Repo.Owner
		headBranch = headInfos[0]

	} else if len(headInfos) == 2 {
		headUser, err = models.GetUserByName(headInfos[0])
		if err != nil {
			log.Trace("Failed to get user by name: %+v", err)
			return nil, nil, nil, nil, "", ""
		}
		headBranch = headInfos[1]
		isSameRepo = headUser.ID == baseRepo.OwnerID

	} else {
		log.Trace("Failed to parse head repo info.")
		return nil, nil, nil, nil, "", ""
	}

	c.Repo.PullRequest.SameRepo = isSameRepo

	// Check if base branch is valid.
	if !c.Repo.GitRepo.IsBranchExist(baseBranch) {
		log.Trace("Base branch: %s does not exist.", baseBranch)
		return nil, nil, nil, nil, "", ""
	}

	var (
		headRepo    *models.Repository
		headGitRepo *git.Repository
	)

	// In case user included redundant head user name for comparison in same repository,
	// no need to check the fork relation.
	if !isSameRepo {
		var has bool
		headRepo, has, err = models.HasForkedRepo(headUser.ID, baseRepo.ID)
		if err != nil {
			log.Trace("Failed to check repo: %s if has been forked with error: %+v", baseRepo.Name, err)
			return nil, nil, nil, nil, "", ""
		} else if !has {
			log.Trace("ParseCompareInfo [base_repo_id: %d]: does not have fork or in same repository", baseRepo.ID)
			return nil, nil, nil, nil, "", ""
		}

		headGitRepo, err = git.OpenRepository(models.RepoPath(headUser.Name, headRepo.Name))
		if err != nil {
			log.Trace("Failed to open repository: %s to user %s with error: %+v", headRepo.Name, headUser.Name, err)
			return nil, nil, nil, nil, "", ""
		}
	} else {
		headRepo = c.Repo.Repository
		headGitRepo = c.Repo.GitRepo
	}

	if !c.User.IsWriterOfRepo(headRepo) && !c.User.IsAdmin {
		log.Trace("ParseCompareInfo [base_repo_id: %d]: does not have write access or site admin", baseRepo.ID)
		return nil, nil, nil, nil, "", ""
	}

	// Check if head branch is valid.
	if !headGitRepo.IsBranchExist(headBranch) {
		log.Trace("Head branch %s is invalid.", headBranch)
		return nil, nil, nil, nil, "", ""
	}

	prInfo, err := headGitRepo.GetPullRequestInfo(models.RepoPath(baseRepo.Owner.Name, baseRepo.Name), baseBranch, headBranch)
	if err != nil {
		if git.IsErrNoMergeBase(err) {
			log.Trace("The PR has no merge base repo.")
		} else {
			log.Trace("Failed to get pull request info: %+v", err)
		}
		return nil, nil, nil, nil, "", ""
	}
	return headUser, headRepo, headGitRepo, prInfo, baseBranch, headBranch
}

func RetrieveRepoMilestonesAndAssignees(c *context.APIContext, repo *models.Repository) {
	var err error
	_, err = models.GetMilestones(repo.ID, -1, false)
	if err != nil {
		c.Error(500, "GetMilestones", err)
		return
	}
	_, err = models.GetMilestones(repo.ID, -1, true)
	if err != nil {
		c.Error(500, "GetMilestones", err)
		return
	}
	_, err = repo.GetAssignees()
	if err != nil {
		c.Error(500, "GetAssignees", err)
		return
	}
}

func RetrieveRepoMetas(c *context.APIContext, repo *models.Repository) []*models.Label {
	if !c.Repo.IsWriter() {
		return nil
	}

	labels, err := models.GetLabelsByRepoID(repo.ID)
	if err != nil {
		c.Error(500, "GetLabelsByRepoID", err)
		return nil
	}

	RetrieveRepoMilestonesAndAssignees(c, repo)
	if c.Written() {
		return nil
	}
	return labels
}

func ValidateRepoMetas(c *context.APIContext, f api.CreateIssueOption) ([]int64, int64, int64) {
	var (
		repo = c.Repo.Repository
		err  error
	)

	labels := RetrieveRepoMetas(c, c.Repo.Repository)
	if !c.Repo.IsWriter() {
		return nil, 0, 0
	}

	// Check labels.
	labelIDs := tool.StringsToInt64s(strings.Split(f.LabelIDs, ","))
	labelIDMark := tool.Int64sToMap(labelIDs)
	hasSelected := false
	for i := range labels {
		if labelIDMark[labels[i].ID] {
			labels[i].IsChecked = true
			hasSelected = true
		}
	}
	log.Trace("The repo has selected label: %+v with label ids: %+v and labels: %+v", hasSelected, labelIDs, labels)

	// Check milestone.
	milestoneID := f.Milestone
	if milestoneID > 0 {
		_, err = repo.GetMilestoneByID(milestoneID)
		if err != nil {
			c.Error(500, "Failed to get milestone by ID: %+v", err)
			return nil, 0, 0
		}
	}

	// Check assignee.
	assigneeID := f.AssigneeID
	if assigneeID > 0 {
		_, err = repo.GetAssigneeByID(assigneeID)
		if err != nil {
			c.Error(500, "Failed to get assignee by ID: %+v", err)
			return nil, 0, 0
		}
	}
	return labelIDs, milestoneID, assigneeID
}

func PrepareCompareDiff(
	c *context.APIContext,
	headUser *models.User,
	headRepo *models.Repository,
	headGitRepo *git.Repository,
	prInfo *git.PullRequestInfo,
	baseBranch, headBranch string) bool {

	var err error

	// Get diff information.
	headCommitID, err := headGitRepo.GetBranchCommitID(headBranch)
	if err != nil {
		log.Trace("Failed to get branch commit ID: %+v", err)
		return false
	}

	if headCommitID == prInfo.MergeBase {
		log.Trace("Nothing to compare with head commit: %s and base commit: %s", headCommitID, prInfo.MergeBase)
		return true
	}
	return false
}

type pullRequestInfo struct {
	IssueID int64 `json:"issue_id"`
	Index   int64 `json:"index"`
}

func CreatePullRequest(c *context.APIContext, form api.CreateIssueOption) {

	var (
		repo        = c.Repo.Repository
		attachments []string
	)

	headUser, headRepo, headGitRepo, prInfo, baseBranch, headBranch := ParseCompareInfo(c)

	if headUser == nil || headRepo == nil || headGitRepo == nil ||
		prInfo == nil || baseBranch == "" || headBranch == "" {
		c.Error(400, "Failed to parse compare info", "Bad request parameters about compare info.")
		return
	}

	pr, err := models.GetUnmergedPullRequest(headRepo.ID, c.Repo.Repository.ID, headBranch, baseBranch)
	if err != nil {
		if !models.IsErrPullRequestNotExist(err) {
			c.Error(500, "GetUnmergedPullRequest", err)
			return
		}
	} else if pr != nil {
		c.JSON(409, &pullRequestInfo{IssueID: pr.IssueID, Index: pr.Index})
		return
	}

	nothingToCompare := PrepareCompareDiff(c, headUser, headRepo, headGitRepo, prInfo, baseBranch, headBranch)
	if nothingToCompare {
		c.Error(412, "Nothing to compare", "There is no diff between two repo with branches.")
		return
	}

	labelIDs, milestoneID, assigneeID := ValidateRepoMetas(c, form)

	if setting.AttachmentEnabled {
		attachments = []string{}
	}

	if c.HasError() {
		c.Error(412, "Context has error", nil)
		return
	}

	patch, err := headGitRepo.GetPatch(prInfo.MergeBase, headBranch)
	if err != nil {
		c.Error(500, "GetPatch", err)
		return
	}

	pullIssue := &models.Issue{
		RepoID:      repo.ID,
		Index:       repo.NextIssueIndex(),
		Title:       form.Title,
		PosterID:    c.User.ID,
		Poster:      c.User,
		MilestoneID: milestoneID,
		AssigneeID:  assigneeID,
		IsPull:      true,
		Content:     form.Body,
	}

	pullRequest := &models.PullRequest{
		HeadRepoID:   headRepo.ID,
		BaseRepoID:   repo.ID,
		HeadUserName: headUser.Name,
		HeadBranch:   headBranch,
		BaseBranch:   baseBranch,
		HeadRepo:     headRepo,
		BaseRepo:     repo,
		MergeBase:    prInfo.MergeBase,
		Type:         models.PULL_REQUEST_GOGS,
	}

	// FIXME: check error in the case two people send pull request at almost same time, give nice error prompt
	// instead of 500.
	if err := models.NewPullRequest(repo, pullIssue, labelIDs, attachments, pullRequest, patch); err != nil {
		c.Error(500, "NewPullRequest", err)
		return
	} else if err := pullRequest.PushToBaseRepo(); err != nil {
		c.Error(500, "PUshToBaseRepo", err)
		return
	}

	log.Trace("Pull request created: %d/%d", repo.ID, pullIssue.ID)
	c.JSON(201, pullRequestInfo{IssueID: pullIssue.ID, Index: pullIssue.Index})

}
