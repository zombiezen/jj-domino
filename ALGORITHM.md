# Algorithm

## Overview

1. **Find candidate heads**: Query all heads in `ancestors(bookmarks()) ~ ::trunk()`.

2. **Build the commit tree**: Walk backwards from each head, tracking which
   changesets have bookmarks (these are submission candidates).

3. **Handle merge commits**: Merge commits are permitted only if all paths
   reunify before reaching the next bookmark or `trunk()`. For example,
   `A→(B, C)→D` is valid if B, C, and D are not in `trunk()` and no
   intervening bookmarks exist. Otherwise, halt the walk for that path.

4. **Select a bookmark**: Prompt the user to select exactly one bookmark. This
   bookmark represents the furthest descendant they wish to submit; ancestral
   bookmarks will be managed automatically.

5. **Process the selected stack**: The stack includes the selected bookmark and
   all ancestor commits with bookmarks (excluding `trunk()`).

   1. **Ensure bookmarks are pushed**: Verify all implicated bookmarks have been
      pushed and are up-to-date. If not, offer to push for the user. Abort if
      they decline.

   2. **Create or update PRs**: For each bookmark, starting with the oldest
      ancestor:

      1. Check if an open PR exists for that bookmark name. (If the bookmark was
         renamed, the old PR is intentionally abandoned—users may discard PRs by
         renaming or deleting the remote bookmark.)

      2. If no PR exists, create one:
         - The oldest bookmark targets `trunk()`.
         - All others target the PR for the next-oldest bookmark, forming a
           stack.

      3. If a PR exists, verify the merge target is correct (next-oldest
         ancestor's PR, or `trunk()` for the oldest).

   3. **Maintain stack comments**: For each PR, look for a comment containing
      the marker `<!-- jj-domino -->`.

      1. If found, update it to reflect the current stack.
      2. If not found, add one.

## Error Handling

If any GitHub API call fails, abort immediately. No state recovery is attempted.

## PR Stack Comment

The stack comment format:

```markdown
<!-- jj-domino -->
This PR is part of a stack:

1. [title of bottom PR](link to PR)
2. [title of next PR](link to PR)
3. _This PR_
4. [title of child PR](link to PR)
5. ...
```

## Definitions

- **`trunk()`**: A built-in Jujutsu revset alias, typically resolving to the
  remote HEAD (`main` or `master`).
- **Bookmark**: A Jujutsu bookmark (analogous to a Git branch) marking a
  commit for submission.
