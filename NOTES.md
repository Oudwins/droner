# Linking worktrees with their parent project

We can do worktree names as
```bash
~/.droner/projectname#worktree1
```


## GETTING THE PATH TO THE PARENT REPO
- git rev-parse --git-common-dir  
  Returns the path to the shared .git directory (the original repo’s .git). Works from any worktree.
- .git file inside the worktree  
  It contains gitdir: /path/to/original/.git/worktrees/<name>. You can parse this to get the original.
- git worktree list --porcelain (run from original repo)  
  Lists all worktrees and their paths, branches, and the main repo.
