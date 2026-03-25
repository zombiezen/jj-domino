package main

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/shurcooL/githubv4"
	"zombiezen.com/go/jj-domino/internal/jujutsu"
)

func TestWriteStackFooter(t *testing.T) {
	repository := &gitHubRepository{
		Owner: &gitHubRepositoryOwner{Login: "zombiezen"},
		Name:  "jj-domino",
	}

	tests := []struct {
		name  string
		stack []stackedDiff
		plan  []*plannedPullRequest
		want  []string
	}{
		{
			name: "Single",
			stack: []stackedDiff{
				{
					localCommitRef: localCommitRef{
						name:   "foo",
						commit: &jujutsu.Commit{ID: jujutsu.CommitID{0x01, 0x23}},
					},
				},
			},
			plan: []*plannedPullRequest{
				{
					baseRepositoryPath: repository.path(),
					pullRequest: pullRequest{
						Number:         123,
						BaseRefName:    "main",
						HeadRefName:    "foo",
						HeadRepository: repository,
					},
				},
			},
			want: []string{
				"",
			},
		},
		{
			name: "TwoStack",
			stack: []stackedDiff{
				{
					localCommitRef: localCommitRef{
						name:   "foo",
						commit: &jujutsu.Commit{ID: jujutsu.CommitID{0x01, 0x23}},
					},
				},
				{
					localCommitRef: localCommitRef{
						name:   "bar",
						commit: &jujutsu.Commit{ID: jujutsu.CommitID{0x45, 0x67}},
					},
				},
			},
			plan: []*plannedPullRequest{
				{
					baseRepositoryPath: repository.path(),
					pullRequest: pullRequest{
						Number:         123,
						BaseRefName:    "main",
						HeadRefName:    "foo",
						HeadRepository: repository,
						URL: githubv4.URI{URL: &url.URL{
							Scheme: "https",
							Host:   "github.com",
							Path:   "/" + repository.path().String() + "/pull/123",
						}},
					},
				},
				{
					baseRepositoryPath: repository.path(),
					pullRequest: pullRequest{
						Number:         456,
						BaseRefName:    "main",
						HeadRefName:    "bar",
						HeadRepository: repository,
						URL: githubv4.URI{URL: &url.URL{
							Scheme: "https",
							Host:   "github.com",
							Path:   "/" + repository.path().String() + "/pull/456",
						}},
					},
				},
			},
			want: []string{
				stackFooterPreamble +
					stackFooterStackIntro +
					" 1. *→ this pull request ←*\n" +
					" 2. #456\n",
				stackFooterPreamble +
					fmt.Sprintf(stackFooterChangesSection, "#123", "commit", "https://github.com/"+repository.path().String()+"/pull/456/changes/4567") +
					stackFooterStackIntro +
					" 1. #123\n" +
					" 2. *→ this pull request ←*\n",
			},
		},
		{
			name: "TwoStackWithMultipleCommits",
			stack: []stackedDiff{
				{
					localCommitRef: localCommitRef{
						name:   "foo",
						commit: &jujutsu.Commit{ID: jujutsu.CommitID{0x01, 0x23}},
					},
				},
				{
					localCommitRef: localCommitRef{
						name:   "bar",
						commit: &jujutsu.Commit{ID: jujutsu.CommitID{0x89, 0xab}},
					},
					uniqueAncestors: []*jujutsu.Commit{
						{ID: jujutsu.CommitID{0x45, 0x67}},
					},
				},
			},
			plan: []*plannedPullRequest{
				{
					baseRepositoryPath: repository.path(),
					pullRequest: pullRequest{
						Number:         123,
						BaseRefName:    "main",
						HeadRefName:    "foo",
						HeadRepository: repository,
						URL: githubv4.URI{URL: &url.URL{
							Scheme: "https",
							Host:   "github.com",
							Path:   "/" + repository.path().String() + "/pull/123",
						}},
					},
				},
				{
					baseRepositoryPath: repository.path(),
					pullRequest: pullRequest{
						Number:         456,
						BaseRefName:    "main",
						HeadRefName:    "bar",
						HeadRepository: repository,
						URL: githubv4.URI{URL: &url.URL{
							Scheme: "https",
							Host:   "github.com",
							Path:   "/" + repository.path().String() + "/pull/456",
						}},
					},
				},
			},
			want: []string{
				stackFooterPreamble +
					stackFooterStackIntro +
					" 1. *→ this pull request ←*\n" +
					" 2. #456\n",
				stackFooterPreamble +
					fmt.Sprintf(stackFooterChangesSection, "#123", "2 commits", "https://github.com/"+repository.path().String()+"/pull/456/changes/4567..89ab") +
					stackFooterStackIntro +
					" 1. #123\n" +
					" 2. *→ this pull request ←*\n",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for i, want := range test.want {
				sb := new(strings.Builder)
				writeStackFooter(sb, new(test.stack[i]), test.plan, i)
				got := sb.String()
				if diff := cmp.Diff(want, got); diff != "" {
					t.Errorf("footer for stack[%d] (-want +got):\n%s", i, diff)
				}
			}
		})
	}
}

func TestTrimStackFooter(t *testing.T) {
	tests := []struct {
		s    string
		want string
	}{
		{"", ""},
		{"Hello, World!", "Hello, World!"},
		{"foo\nbar", "foo\nbar"},
		{
			"I can mention " + stackFooterMarker + " in the middle of a line.",
			"I can mention " + stackFooterMarker + " in the middle of a line.",
		},
		{
			"foo\n" + stackFooterMarker + "\nbar",
			"foo\n",
		},
		{
			"foo\r\n" + stackFooterMarker + "\r\nbar",
			"foo\r\n",
		},
		{
			"foo\n  " + stackFooterMarker + " \nbar",
			"foo\n",
		},
	}

	for _, test := range tests {
		got := trimStackFooter(test.s)
		if diff := cmp.Diff(test.want, got); diff != "" {
			t.Errorf("trimStackFooter(%q) (-want +got:)\n%s", test.s, diff)
		}
	}
}

func TestFormatPRNumber(t *testing.T) {
	tests := []struct {
		n     githubv4.Int
		width int
		want  string
	}{
		{1, 1, "#1"},
		{2, 1, "#2"},
		{1, 2, " #1"},
		{123, 3, "#123"},
		{123, 1, "#123"},
		{123, 5, "  #123"},
	}

	for _, test := range tests {
		if got := formatPRNumber(test.n, test.width); got != test.want {
			t.Errorf("formatPRNumber(%d, %d) = %q; want %q", test.n, test.width, got, test.want)
		}
	}
}

func TestPRNumberPlaceholder(t *testing.T) {
	tests := []struct {
		width int
		want  string
	}{
		{1, "#X"},
		{3, "#XXX"},
	}

	for _, test := range tests {
		if got := prNumberPlaceholder(test.width); got != test.want {
			t.Errorf("prNumberPlaceholder(%d) = %q; want %q", test.width, got, test.want)
		}
	}
}
