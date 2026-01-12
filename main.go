package main

import (
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
	return nil
}

func main() {
	ctx := kong.Parse(&cli{}, kong.UsageOnError())
	if err := ctx.Run(); err != nil {
		log.Fatal(err)
	}
}
