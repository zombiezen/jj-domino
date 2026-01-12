# Algorithm

## Overview

Here's how I think this should work:

1. Grab all heads in `mine() ~ trunk()`
2. Walk them backwards, building up a tree. Track which changesets have
   bookmarks; they're submission candidates.
3. If we ever hit a merge commit, we halt the walk for that particular path.
   (Multiple children is fine, just not multiple parents; we're aiming for
   linear ancestry.)
4. Of those bookmarks we hit, ask the user which one they care about. They may
   only select one (though as we'll discuss, ancestral bookmarks may be
   impacted).
5. Once they have selected a bookmark, we're now interested in that bookmark and
   all ancestor commits that we walked.
   1. First, we check that all of those bookmarks have been pushed and are
      up-to-date. If they aren't, we offer to fix this for the user; if they
      decline, abort.
   2. Now, for each bookmark that is implicated, starting with the oldest
      ancestor:
      1. Check if an open PR exists for that changeset. We _only_ care about
         bookmark name for this.
      2. If none does, open it. The oldest will target `trunk()`; any others
         should target the PR for the next-oldest bookmark, making a PR stack.
      3. If the PR does exist, make sure the merge targets are correct (i.e.
         target next-oldest ancestor, or `trunk()` if it's the oldest).
   3. Once all PRs exist with proper parents, check each PR for a comment with
      the comment `<!-- jj-domino -->` hidden in it.
      1. If such a comment does exist, edit it to show the current stack (note
         on format later on)
      2. If no such comment exists, add one

## Tab Stack Comment

The tab stack comment should look like this:

```markdown
This PR is part of a stack:

1. [title of bottom PR in stack](link to PR)
2. [title of next PR in stack](link to PR)
3. _This PR_ <== you are here
4. [title of child PR in stack](link to PR)
5. ...etc...
```
