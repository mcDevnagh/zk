package cmd

import (
	"log"
	"os"

	"github.com/mickael-menu/zk/adapter/handlebars"
	"github.com/mickael-menu/zk/util"
	"github.com/mickael-menu/zk/util/date"
)

type Container struct {
	Date           date.Provider
	Logger         util.Logger
	templateLoader *handlebars.Loader
}

func NewContainer() *Container {
	date := date.NewFrozenNow()

	return &Container{
		Logger: log.New(os.Stderr, "zk: warning: ", 0),
		// zk is short-lived, so we freeze the current date to use the same
		// date for any rendering during the execution.
		Date: &date,
	}
}

func (c *Container) TemplateLoader() *handlebars.Loader {
	if c.templateLoader == nil {
		// FIXME take the language from the config
		handlebars.Init("en", c.Logger, c.Date)
		c.templateLoader = handlebars.NewLoader()
	}
	return c.templateLoader
}