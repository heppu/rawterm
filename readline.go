// Readline is a pure go implementation for GNU-Readline kind library.
//
// example:
// 	rl, err := readline.New("> ")
// 	if err != nil {
// 		panic(err)
// 	}
// 	defer rl.Close()
//
// 	for {
// 		line, err := rl.Readline()
// 		if err != nil { // io.EOF
// 			break
// 		}
// 		println(line)
// 	}
//
package readline

import "io"

type Instance struct {
	Config    *Config
	Terminal  *Terminal
	Operation *Operation
}

type Config struct {
	// prompt supports ANSI escape sequence, so we can color some characters even in windows
	Prompt string

	// Any key press will pass to Listener
	// NOTE: Listener will be triggered by (nil, 0, 0) immediately
	Listener Listener

	InterruptPrompt string
	EOFPrompt       string

	FuncGetWidth func() int

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	EnableMask bool
	MaskRune   rune

	// erase the editing line after user submited it
	// it use in IM usually.
	UniqueEditLine bool

	// filter input runes (may be used to disable CtrlZ or for translating some keys to different actions)
	// -> output = new (translated) rune and true/false if continue with processing this one
	FuncFilterInputRune func(rune) (rune, bool)

	// force use interactive even stdout is not a tty
	FuncIsTerminal      func() bool
	FuncMakeRaw         func() error
	FuncExitRaw         func() error
	FuncOnWidthChanged  func(func())
	ForceUseInteractive bool

	// private fields
	inited bool
}

func (c *Config) useInteractive() bool {
	if c.ForceUseInteractive {
		return true
	}
	return c.FuncIsTerminal()
}

func (c *Config) Init() error {
	if c.inited {
		return nil
	}
	c.inited = true
	if c.Stdin == nil {
		c.Stdin = NewCancelableStdin(Stdin)
	}
	if c.Stdout == nil {
		c.Stdout = Stdout
	}
	if c.Stderr == nil {
		c.Stderr = Stderr
	}

	if c.InterruptPrompt == "" {
		c.InterruptPrompt = "^C"
	} else if c.InterruptPrompt == "\n" {
		c.InterruptPrompt = ""
	}
	if c.EOFPrompt == "" {
		c.EOFPrompt = "^D"
	} else if c.EOFPrompt == "\n" {
		c.EOFPrompt = ""
	}

	if c.FuncGetWidth == nil {
		c.FuncGetWidth = GetScreenWidth
	}
	if c.FuncIsTerminal == nil {
		c.FuncIsTerminal = DefaultIsTerminal
	}
	rm := new(RawMode)
	if c.FuncMakeRaw == nil {
		c.FuncMakeRaw = rm.Enter
	}
	if c.FuncExitRaw == nil {
		c.FuncExitRaw = rm.Exit
	}
	if c.FuncOnWidthChanged == nil {
		c.FuncOnWidthChanged = DefaultOnWidthChanged
	}

	return nil
}

func (c *Config) SetListener(f func(line []rune, pos int, key rune) (newLine []rune, newPos int, ok bool)) {
	c.Listener = FuncListener(f)
}

func NewEx(cfg *Config) (*Instance, error) {
	t, err := NewTerminal(cfg)
	if err != nil {
		return nil, err
	}
	rl := t.Readline()
	return &Instance{
		Config:    cfg,
		Terminal:  t,
		Operation: rl,
	}, nil
}

func New(prompt string) (*Instance, error) {
	return NewEx(&Config{Prompt: prompt})
}

func (i *Instance) SetPrompt(s string) {
	i.Operation.SetPrompt(s)
}

func (i *Instance) SetMaskRune(r rune) {
	i.Operation.SetMaskRune(r)
}

// readline will refresh automatic when write through Stdout()
func (i *Instance) Stdout() io.Writer {
	return i.Operation.Stdout()
}

// readline will refresh automatic when write through Stdout()
func (i *Instance) Stderr() io.Writer {
	return i.Operation.Stderr()
}

func (i *Instance) GenPasswordConfig() *Config {
	return i.Operation.GenPasswordConfig()
}

// we can generate a config by `i.GenPasswordConfig()`
func (i *Instance) ReadPasswordWithConfig(cfg *Config) ([]byte, error) {
	return i.Operation.PasswordWithConfig(cfg)
}

func (i *Instance) ReadPasswordEx(prompt string, l Listener) ([]byte, error) {
	return i.Operation.PasswordEx(prompt, l)
}

func (i *Instance) ReadPassword(prompt string) ([]byte, error) {
	return i.Operation.Password(prompt)
}

type Result struct {
	Line  string
	Error error
}

func (l *Result) CanContinue() bool {
	return len(l.Line) != 0 && l.Error == ErrInterrupt
}

func (l *Result) CanBreak() bool {
	return !l.CanContinue() && l.Error != nil
}

func (i *Instance) Line() *Result {
	ret, err := i.Readline()
	return &Result{ret, err}
}

// err is one of (nil, io.EOF, readline.ErrInterrupt)
func (i *Instance) Readline() (string, error) {
	return i.Operation.String()
}

// same as readline
func (i *Instance) ReadSlice() ([]byte, error) {
	return i.Operation.Slice()
}

// we must make sure that call Close() before process exit.
func (i *Instance) Close() error {
	if err := i.Terminal.Close(); err != nil {
		return err
	}
	return nil
}
func (i *Instance) Clean() {
	i.Operation.Clean()
}

func (i *Instance) Write(b []byte) (int, error) {
	return i.Stdout().Write(b)
}

func (i *Instance) SetConfig(cfg *Config) *Config {
	if i.Config == cfg {
		return cfg
	}
	old := i.Config
	i.Config = cfg
	i.Operation.SetConfig(cfg)
	i.Terminal.SetConfig(cfg)
	return old
}

func (i *Instance) Refresh() {
	i.Operation.Refresh()
}
