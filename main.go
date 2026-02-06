package main

import (
	"context"
	"fmt"
	"log"

	"github.com/alecthomas/kong"
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
	var root string
	if c.Root != nil {
		root = *c.Root
	} else {
		var err error
		root, err = getCurrentRoot(ctx)
		if err != nil {
			return err
		}
	}
	fmt.Printf("root: %#v\n", root)
	r := NewRepository(root)
	changes, err := r.getChangesets(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("%#v\n", changes)
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
	if err := ctx.Run(); err != nil {
		log.Fatal(err)
	}
}
