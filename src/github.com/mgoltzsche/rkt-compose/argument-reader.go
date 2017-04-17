package main

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
)

type CmdArgs struct {
	commands map[string]*cmd
	options  map[string]*option
}

func NewCmdArgs(options interface{}) *CmdArgs {
	a := &CmdArgs{map[string]*cmd{}, toOptions(options)}
	a.AddCmd("help", "Shows this help text", nil, a.helpCommand)
	return a
}

type cmd struct {
	name        string
	description string
	options     map[string]*option
	param       *option
	callback    func() error
}

type option struct {
	description string
	value       reflect.Value
}

func (a *CmdArgs) AddCmd(name, description string, options interface{}, callback func() error) {
	a.commands[name] = &cmd{name, description, toOptions(options), toParam(options), callback}
}

func (a *CmdArgs) Run() error {
	paramSet := false
	var c *cmd
	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		if len(arg) > 2 && arg[0:2] == "--" {
			// Set option
			opt := a.options
			if c != nil {
				opt = c.options
			}
			o := opt[arg]
			if o == nil {
				return fmt.Errorf("Unsupported option: %s", arg)
			}
			i++
			o.value.SetString(os.Args[i])
		} else if c == nil {
			// Set command
			c = a.commands[arg]
			if c == nil {
				return fmt.Errorf("Unsupported command: %q", arg)
			}
		} else {
			// Set params
			if c.param == nil {
				return fmt.Errorf("%s command does not support parameters", c.name)
			}
			if paramSet {
				return fmt.Errorf("%s command does not support multiple %s parameters", c.name, c.param.description)
			}
			c.param.value.SetString(arg)
			paramSet = true
		}
	}
	if c == nil {
		return errors.New("No command provided")
	}
	if c.param != nil && !paramSet {
		return fmt.Errorf("No %s parameter provided to %s command", c.param.description, c.name)
	}
	return c.callback()
}

func (a *CmdArgs) helpCommand() error {
	a.ShowHelp()
	return nil
}

func (a *CmdArgs) ShowHelp() {
	h := fmt.Sprintf("Usage: %s OPTIONS COMMAND\n  COMMAND:\n", os.Args[0])
	for name, c := range a.commands {
		l := name
		if c.param != nil {
			l += " " + c.param.description
		}
		h += fmt.Sprintf("    %-17s %s\n", l, c.description)
		for k, v := range c.options {
			h += fmt.Sprintf("      %-15s %s\n", k, v.description)
		}
	}
	if len(a.options) > 0 {
		h += "  OPTIONS:\n"
		for k, v := range a.options {
			h += fmt.Sprintf("    %-15s %s\n", k, v.description)
		}
	}
	fmt.Print(h)
}

func toParam(options interface{}) *option {
	if options != nil {
		t := reflect.ValueOf(options).Elem()
		for i := 0; i < t.NumField(); i++ {
			f := t.Type().Field(i)
			tag := f.Tag.Get("param")
			if len(tag) > 0 {
				return &option{tag, t.Field(i)}
			}
		}
	}
	return nil
}

func toOptions(options interface{}) map[string]*option {
	opt := map[string]*option{}
	if options != nil {
		t := reflect.ValueOf(options).Elem()
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			tag := t.Type().Field(i).Tag.Get("opt")
			if len(tag) > 0 {
				tagSplit := strings.SplitN(tag, ",", 2)
				optName := tagSplit[0]
				optDescr := ""
				if len(tagSplit) > 1 {
					optDescr = tagSplit[1]
				}
				opt["--"+optName] = &option{optDescr, f}
			}
		}
	}
	return opt
}
