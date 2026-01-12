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
	Draft        bool   `short:"d" help:"Submit PR as draft"`
	TemplatePath string `short:"t" help:"Template path"`
}

type doctorCmd struct{}

func (c *submitCmd) Run() error {
	return nil
}

func (c *doctorCmd) Run() error {
	client, err := getClient()
	if err != nil {
		return err
	}
	user, _, err := client.Users.Get(context.Background(), "")
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
