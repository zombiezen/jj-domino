# jj-domino

jj-domino is a GitHub pull request [stack manager][stacking workflow] for [Jujutsu][].
jj-domino can create pull requests
that are "stacked" on top of each other,
allowing you to send out pull requests that depend on changes in other pull requests
without having to merge them first.

```console
$ jj git clone https://github.com/octocat/Hello-World.git
$ cd Hello-World
$ touch foo.txt
$ jj commit -m "Add foo.txt"
$ touch bar.txt
$ jj commit -m "Add bar.txt"
$ jj-domino -c 'trunk()..@-'
Creating bookmark push-lvlupwyrvtrq for revision lvlupwyrvtrq
Creating bookmark push-vkoqnzswumlq for revision vkoqnzswumlq
Changes to push to origin:
  Add bookmark push-lvlupwyrvtrq to 7f016689053c
  Add bookmark push-vkoqnzswumlq to fd73fcd14312

#1234: Add foo.txt [main ← push-vkoqnzswumlq] (new)
#1235: [DRAFT] Add bar.txt [push-vkoqnzswumlq ← push-lvlupwyrvtrq] (new)
```

[Jujutsu]: https://www.jj-vcs.dev/
[stacking workflow]: https://www.stacking.dev/

## Installation

Assuming you already have [Jujutsu][] installed:

1. [Install Go](https://go.dev/dl/)
2. `go install zombiezen.com/go/jj-domino@latest`
3. Authenticate to GitHub using one of two options:
   - If you're already using the [GitHub CLI][], then run `gh auth login`.
   - Otherwise, [create a personal access token with `repo` scope](https://github.com/settings/tokens/new?scopes=repo)
     and store it in the environment variable `GITHUB_TOKEN`
     or the file `$XDG_CONFIG_DIRS/jj-domino/github-token`.

[GitHub CLI]: https://cli.github.com/

## Basics

A common workflow is to use `jj-domino -c` when first creating the pull request(s).
For example, to create a single pull request:

```console
$ jj new 'trunk()'
$ touch foo.txt
$ jj commit -m 'Add foo.txt'
$ jj-domino -c @-
Creating bookmark push-vkoqnzswumlq for revision vkoqnzswumlq
Changes to push to origin:
  Add bookmark push-vkoqnzswumlq to fd73fcd14312

#1234: Add foo.txt [main ← push-vkoqnzswumlq] (new)
```

Then you can use `jj-domino` without arguments to update the pull request(s).
Without arguments, jj-domino will create or update a pull request for each [bookmark][bookmarks]
in the [revset][revsets] `trunk()..@`.
For example, to incorporate some changes:

```
$ echo "add a line" >> foo.txt
$ jj squash
$ jj-domino
Changes to push to origin:
  Move sideways bookmark push-vkoqnzswumlq from fd73fcd14312 to 0dcc295c3c92

#1234: Add foo.txt [main ← push-vkoqnzswumlq]
```

jj-domino also has other useful options like `--dry-run`.
Run `jj-domino submit --help` to see more documentation.

[bookmarks]: https://docs.jj-vcs.dev/latest/bookmarks/
[revsets]: https://docs.jj-vcs.dev/latest/revsets/
[`jj git push`]: https://docs.jj-vcs.dev/latest/cli-reference/#jj-git-push

## Configuration

jj-domino attempts to work with existing Jujutsu configuration
rather than having its own settings.
For example, jj-domino will infer the GitHub repository based on the configured remotes
and will infer the default base branch using the `trunk()` revset alias.
These settings can be overridden with command-line flags.

## License

[MIT](LICENSE)

