package main

import (
	"context"
	"fmt"
	"log"

	"github.com/alecthomas/kong"
	"zombiezen.com/go/jj-domino/internal/jujutsu"
)

type cli struct {
	Submit submitCmd `cmd:"" help:"Submit a review stack"`
	Doctor doctorCmd `cmd:"" help:"Verify auth and config settings"`
}

type submitCmd struct {
	Draft        bool    `short:"d" help:"Submit PR as draft"`
	TemplatePath *string `short:"t" help:"Template path"`
	Root         *string `short:"R" help:"Optional repository root (defaults to \"jj root\")"`
}

type doctorCmd struct{}

func (c *submitCmd) Run(ctx context.Context) error {
	opts := jujutsu.Options{}
	if c.Root != nil {
		opts.Dir = *c.Root
	}
	jj, err := jujutsu.New(opts)
	if err != nil {
		return err
	}
	root, err := jj.WorkspaceRoot(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("root: %#v\n", root)
	bookmarks, err := jj.ListBookmarks(ctx)
	if err != nil {
		return err
	}
	var changes []*jujutsu.Commit
	err = jj.Log(ctx, "mutable() & (ancestors(bookmarks()) ~ ::trunk())", func(c *jujutsu.Commit) bool {
		changes = append(changes, c)
		return true
	})
	if err != nil {
		return err
	}
	for _, c := range changes {
		fmt.Print(c.ChangeID.Short())
		for _, b := range bookmarks {
			if target, ok := b.TargetMerge.Resolved(); b.Remote == "" && ok && target.Equal(c.ID) {
				fmt.Print(" " + b.Name)
			}
		}
		fmt.Printf(" (%v)\n%s\n\n", c.ID, c.Description)
	}
	return nil
}

func (c *doctorCmd) Run(ctx context.Context) error {
	client, err := getClient()
	if err != nil {
		return err
	}
	user, _, err := client.Users.Get(ctx, "")
	if err != nil {
		return err
	}
	fmt.Printf("Authenticated as: %s\n", user.GetLogin())
	return nil
}

func main() {
	ctx := kong.Parse(&cli{}, kong.UsageOnError())
	ctx.BindTo(context.Background(), (*context.Context)(nil))
	if err := ctx.Run(); err != nil {
		log.Fatal(err)
	}
}
