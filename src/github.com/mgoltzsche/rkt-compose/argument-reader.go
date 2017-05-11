package main

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
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
	description  string
	defaultValue string
	value        reflect.Value
	set          func(value reflect.Value, v string) error
}

func (a *CmdArgs) AddCmd(name, description string, options interface{}, callback func() error) {
	a.commands[name] = &cmd{name, description, toOptions(options), toParam(options), callback}
}

func (a *CmdArgs) Run() error {
	paramSet := false
	var c *cmd
	ac := len(os.Args)
	for i := 1; i < ac; i++ {
		arg := os.Args[i]
		if len(arg) > 2 && arg[0:2] == "--" {
			// Set option
			var optName, optVal string
			eqPos := strings.Index(arg, "=")
			if eqPos > 2 {
				optName = arg[:eqPos]
				optVal = arg[eqPos+1:]
			} else {
				optName = arg
				i++
				if i < ac {
					optVal = os.Args[i]
				} else {
					optVal = ""
				}
			}
			opt := a.options
			if c != nil {
				opt = c.options
			}
			o := opt[optName]
			if o == nil {
				return fmt.Errorf("Unsupported option: %s", optName)
			}
			if i == ac {
				return fmt.Errorf("No value provided for option %s", optName)
			}
			if err := o.set(o.value, optVal); err != nil {
				return fmt.Errorf("Invalid option %s - %s", optName, err)
			}
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
			if err := c.param.set(c.param.value, arg); err != nil {
				return fmt.Errorf("Invalid param %s to command %s - %s", c.param.description, c.name, err)
			}
			paramSet = true
		}
	}
	if c == nil {
		return fmt.Errorf("No command provided. Use `%s help` to list available commands", os.Args[0])
	}
	if c.param != nil && len(c.param.defaultValue) == 0 && !paramSet {
		return fmt.Errorf("No %s parameter provided to %s command", c.param.description, c.name)
	}
	return c.callback()
}

func (a *CmdArgs) helpCommand() error {
	a.ShowHelp()
	return nil
}

func (a *CmdArgs) ShowHelp() {
	h := fmt.Sprintf("Usage: %s OPTIONS COMMAND\nCOMMAND:\n", os.Args[0])
	for name, c := range a.commands {
		l := name
		if c.param != nil {
			l += " "
			if len(c.param.defaultValue) > 0 {
				l += "[" + c.param.description + "]"
			} else {
				l += c.param.description
			}
		}
		h += fmt.Sprintf("  %-25s %s\n", l, c.description)
		for k, v := range c.options {
			o := k + "=" + v.defaultValue
			h += fmt.Sprintf("    %-23s %s\n", o, v.description)
		}
	}
	if len(a.options) > 0 {
		h += "OPTIONS:\n"
		for k, v := range a.options {
			o := k + "=" + v.defaultValue
			h += fmt.Sprintf("  %-25s %s\n", o, v.description)
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
				tagSplit := strings.SplitN(tag, ",", 2)
				if len(tagSplit) != 2 {
					panic("Invalid tag on param field '" + f.Name + "': " + tag + ". Required: param,default")
				}
				return toOption(tagSplit[1], tagSplit[0], t.Field(i))
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
			f := t.Type().Field(i)
			tag := f.Tag.Get("opt")
			if len(tag) > 0 {
				tagSplit := strings.SplitN(tag, ",", 3)
				if len(tagSplit) != 3 {
					panic("Invalid tag on options field '" + f.Name + "': " + tag + ". Required: opt,default,description")
				}
				optName := tagSplit[0]
				optZero := tagSplit[1]
				optDescr := tagSplit[2]
				opt["--"+optName] = toOption(optZero, optDescr, t.Field(i))
			}
		}
	}
	return opt
}

func toOption(defaultValue string, descr string, f reflect.Value) *option {
	init := true
	var o *option
	switch f.Interface().(type) {
	case string:
		o = &option{descr, defaultValue, f, setString}
	case bool:
		o = &option{descr, defaultValue, f, setBool}
	case time.Duration:
		o = &option{descr, defaultValue, f, setDuration}
	case []string:
		init = false
		f.Set(reflect.ValueOf([]string{}))
		o = &option{descr, defaultValue, f, setStringArray}
	default:
		panic("Unsupported option field type. Supported types are string, bool and time.Duration")
	}
	if init {
		err := o.set(o.value, defaultValue)
		if err != nil {
			panic("Invalid default value on option of type " + f.Type().String())
		}
	}
	return o
}

func setString(value reflect.Value, str string) error {
	value.SetString(str)
	return nil
}

func setStringArray(value reflect.Value, str string) error {
	value.Set(reflect.Append(value, reflect.ValueOf(str)))
	return nil
}

func setBool(value reflect.Value, str string) error {
	b, e := strconv.ParseBool(str)
	if e != nil {
		return e
	}
	value.SetBool(b)
	return nil
}

func setDuration(value reflect.Value, str string) error {
	d, e := time.ParseDuration(str)
	if e != nil {
		return e
	}
	value.SetInt(int64(d))
	return nil
}
