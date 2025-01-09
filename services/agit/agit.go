// Copyright 2021 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package agit

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	issues_model "code.gitea.io/gitea/models/issues"
	repo_model "code.gitea.io/gitea/models/repo"
	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/git"
	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/private"
	"code.gitea.io/gitea/modules/setting"
	notify_service "code.gitea.io/gitea/services/notify"
	pull_service "code.gitea.io/gitea/services/pull"
)

type updateExistPullOption struct {
	ctx       context.Context
	pr        *issues_model.PullRequest
	gitRepo   *git.Repository
	repo      *repo_model.Repository
	forcePush bool
	pusher    *user_model.User

	RefFullName git.RefName
	OldCommitID string
	NewCommitID string
}

func updateExistPull(opts *updateExistPullOption) (*private.HookProcReceiveRefResult, error) {
	// update exist pull request
	if err := opts.pr.LoadBaseRepo(opts.ctx); err != nil {
		return nil, fmt.Errorf("unable to load base repository for PR[%d] Error: %w", opts.pr.ID, err)
	}

	oldCommitID, err := opts.gitRepo.GetRefCommitID(opts.pr.GetGitRefName())
	if err != nil {
		return nil, fmt.Errorf("unable to get ref commit id in base repository for PR[%d] Error: %w", opts.pr.ID, err)
	}

	if oldCommitID == opts.NewCommitID {
		return &private.HookProcReceiveRefResult{
			OriginalRef: opts.RefFullName,
			OldOID:      opts.OldCommitID,
			NewOID:      opts.NewCommitID,
			Err:         "new commit is same with old commit",
		}, nil
	}

	if !opts.forcePush {
		output, _, err := git.NewCommand(opts.ctx, "rev-list", "--max-count=1").
			AddDynamicArguments(oldCommitID, "^"+opts.NewCommitID).
			RunStdString(&git.RunOpts{Dir: opts.repo.RepoPath(), Env: os.Environ()})
		if err != nil {
			return nil, fmt.Errorf("failed to detect force push: %w", err)
		} else if len(output) > 0 {
			return &private.HookProcReceiveRefResult{
				OriginalRef: opts.RefFullName,
				OldOID:      opts.OldCommitID,
				NewOID:      opts.NewCommitID,
				Err:         "request `force-push` push option",
			}, nil
		}
	}

	opts.pr.HeadCommitID = opts.NewCommitID
	if err = pull_service.UpdateRef(opts.ctx, opts.pr); err != nil {
		return nil, fmt.Errorf("failed to update pull ref. Error: %w", err)
	}

	pull_service.AddToTaskQueue(opts.ctx, opts.pr)
	err = opts.pr.LoadIssue(opts.ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load pull issue. Error: %w", err)
	}
	comment, err := pull_service.CreatePushPullComment(opts.ctx, opts.pusher, opts.pr, oldCommitID, opts.NewCommitID)
	if err == nil && comment != nil {
		notify_service.PullRequestPushCommits(opts.ctx, opts.pusher, opts.pr, comment)
	}
	notify_service.PullRequestSynchronized(opts.ctx, opts.pusher, opts.pr)
	isForcePush := comment != nil && comment.IsForcePush

	return &private.HookProcReceiveRefResult{
		OldOID:            oldCommitID,
		NewOID:            opts.NewCommitID,
		Ref:               opts.pr.GetGitRefName(),
		OriginalRef:       opts.RefFullName,
		IsForcePush:       isForcePush,
		IsCreatePR:        false,
		URL:               fmt.Sprintf("%s/pulls/%d", opts.repo.HTMLURL(), opts.pr.Index),
		ShouldShowMessage: setting.Git.PullRequestPushMessage && opts.repo.AllowsPulls(opts.ctx),
	}, nil
}

// ProcReceive handle proc receive work
func ProcReceive(ctx context.Context, repo *repo_model.Repository, gitRepo *git.Repository, opts *private.HookOptions) ([]private.HookProcReceiveRefResult, error) {
	results := make([]private.HookProcReceiveRefResult, 0, len(opts.OldCommitIDs))
	forcePush := opts.GitPushOptions.Bool(private.GitPushOptionForcePush)
	topicBranch := opts.GitPushOptions["topic"]
	title := strings.TrimSpace(opts.GitPushOptions["title"])
	description := strings.TrimSpace(opts.GitPushOptions["description"])
	objectFormat := git.ObjectFormatFromName(repo.ObjectFormatName)
	userName := strings.ToLower(opts.UserName)

	pusher, err := user_model.GetUserByID(ctx, opts.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user. Error: %w", err)
	}

	for i := range opts.OldCommitIDs {
		if opts.NewCommitIDs[i] == objectFormat.EmptyObjectID().String() {
			results = append(results, private.HookProcReceiveRefResult{
				OriginalRef: opts.RefFullNames[i],
				OldOID:      opts.OldCommitIDs[i],
				NewOID:      opts.NewCommitIDs[i],
				Err:         "Can't delete not exist branch",
			})
			continue
		}

		if opts.RefFullNames[i].IsForReview() {
			// try match refs/for-review/<pull index>
			pullIndex, err := strconv.ParseInt(strings.TrimPrefix(string(opts.RefFullNames[i]), git.ForReviewPrefix), 10, 64)
			if err != nil {
				results = append(results, private.HookProcReceiveRefResult{
					OriginalRef: opts.RefFullNames[i],
					OldOID:      opts.OldCommitIDs[i],
					NewOID:      opts.NewCommitIDs[i],
					Err:         "Unknow pull request index",
				})
				continue
			}
			log.Trace("Pull request index: %d", pullIndex)
			pull, err := issues_model.GetPullRequestByIndex(ctx, repo.ID, pullIndex)
			if err != nil {
				results = append(results, private.HookProcReceiveRefResult{
					OriginalRef: opts.RefFullNames[i],
					OldOID:      opts.OldCommitIDs[i],
					NewOID:      opts.NewCommitIDs[i],
					Err:         "Unknow pull request index",
				})
				continue
			}

			result, err := updateExistPull(&updateExistPullOption{
				ctx:         ctx,
				pr:          pull,
				gitRepo:     gitRepo,
				repo:        repo,
				forcePush:   forcePush.Value(),
				pusher:      pusher,
				RefFullName: opts.RefFullNames[i],
				OldCommitID: opts.OldCommitIDs[i],
				NewCommitID: opts.NewCommitIDs[i],
			})
			if err != nil {
				return nil, err
			}
			results = append(results, *result)

			continue
		}

		if !opts.RefFullNames[i].IsFor() {
			results = append(results, private.HookProcReceiveRefResult{
				IsNotMatched: true,
				OriginalRef:  opts.RefFullNames[i],
			})
			continue
		}

		baseBranchName := opts.RefFullNames[i].ForBranchName()
		currentTopicBranch := ""
		if !gitRepo.IsBranchExist(baseBranchName) {
			// try match refs/for/<target-branch>/<topic-branch>
			for p, v := range baseBranchName {
				if v == '/' && gitRepo.IsBranchExist(baseBranchName[:p]) && p != len(baseBranchName)-1 {
					currentTopicBranch = baseBranchName[p+1:]
					baseBranchName = baseBranchName[:p]
					break
				}
			}
		}

		if len(topicBranch) == 0 && len(currentTopicBranch) == 0 {
			results = append(results, private.HookProcReceiveRefResult{
				OriginalRef: opts.RefFullNames[i],
				OldOID:      opts.OldCommitIDs[i],
				NewOID:      opts.NewCommitIDs[i],
				Err:         "topic-branch is not set",
			})
			continue
		}

		if len(currentTopicBranch) == 0 {
			currentTopicBranch = topicBranch
		}

		// because different user maybe want to use same topic,
		// So it's better to make sure the topic branch name
		// has username prefix
		var headBranch string
		if !strings.HasPrefix(currentTopicBranch, userName+"/") {
			headBranch = userName + "/" + currentTopicBranch
		} else {
			headBranch = currentTopicBranch
		}

		pr, err := issues_model.GetUnmergedPullRequest(ctx, repo.ID, repo.ID, headBranch, baseBranchName, issues_model.PullRequestFlowAGit)
		if err != nil {
			if !issues_model.IsErrPullRequestNotExist(err) {
				return nil, fmt.Errorf("failed to get unmerged agit flow pull request in repository: %s Error: %w", repo.FullName(), err)
			}

			var commit *git.Commit
			if title == "" || description == "" {
				commit, err = gitRepo.GetCommit(opts.NewCommitIDs[i])
				if err != nil {
					return nil, fmt.Errorf("failed to get commit %s in repository: %s Error: %w", opts.NewCommitIDs[i], repo.FullName(), err)
				}
			}

			// create a new pull request
			if title == "" {
				title = strings.Split(commit.CommitMessage, "\n")[0]
			}
			if description == "" {
				_, description, _ = strings.Cut(commit.CommitMessage, "\n\n")
			}
			if description == "" {
				description = title
			}

			prIssue := &issues_model.Issue{
				RepoID:   repo.ID,
				Title:    title,
				PosterID: pusher.ID,
				Poster:   pusher,
				IsPull:   true,
				Content:  description,
			}

			pr := &issues_model.PullRequest{
				HeadRepoID:   repo.ID,
				BaseRepoID:   repo.ID,
				HeadBranch:   headBranch,
				HeadCommitID: opts.NewCommitIDs[i],
				BaseBranch:   baseBranchName,
				HeadRepo:     repo,
				BaseRepo:     repo,
				MergeBase:    "",
				Type:         issues_model.PullRequestGitea,
				Flow:         issues_model.PullRequestFlowAGit,
			}
			prOpts := &pull_service.NewPullRequestOptions{
				Repo:        repo,
				Issue:       prIssue,
				PullRequest: pr,
			}
			if err := pull_service.NewPullRequest(ctx, prOpts); err != nil {
				return nil, err
			}

			log.Trace("Pull request created: %d/%d", repo.ID, prIssue.ID)

			results = append(results, private.HookProcReceiveRefResult{
				Ref:               pr.GetGitRefName(),
				OriginalRef:       opts.RefFullNames[i],
				OldOID:            objectFormat.EmptyObjectID().String(),
				NewOID:            opts.NewCommitIDs[i],
				IsCreatePR:        true,
				URL:               fmt.Sprintf("%s/pulls/%d", repo.HTMLURL(), pr.Index),
				ShouldShowMessage: setting.Git.PullRequestPushMessage && repo.AllowsPulls(ctx),
				HeadBranch:        headBranch,
			})
			continue
		}

		result, err := updateExistPull(&updateExistPullOption{
			ctx:         ctx,
			pr:          pr,
			gitRepo:     gitRepo,
			repo:        repo,
			forcePush:   forcePush.Value(),
			pusher:      pusher,
			RefFullName: opts.RefFullNames[i],
			OldCommitID: opts.OldCommitIDs[i],
			NewCommitID: opts.NewCommitIDs[i],
		})
		if err != nil {
			return nil, err
		}
		results = append(results, *result)
	}

	return results, nil
}

// UserNameChanged handle user name change for agit flow pull
func UserNameChanged(ctx context.Context, user *user_model.User, newName string) error {
	pulls, err := issues_model.GetAllUnmergedAgitPullRequestByPoster(ctx, user.ID)
	if err != nil {
		return err
	}

	newName = strings.ToLower(newName)

	for _, pull := range pulls {
		pull.HeadBranch = strings.TrimPrefix(pull.HeadBranch, user.LowerName+"/")
		pull.HeadBranch = newName + "/" + pull.HeadBranch
		if err = pull.UpdateCols(ctx, "head_branch"); err != nil {
			return err
		}
	}

	return nil
}
