package rawterm

import (
	"errors"
	"io"
)

var (
	ErrInterrupt = errors.New("Interrupt")
)

type InterruptError struct {
	Line []rune
}

func (*InterruptError) Error() string {
	return "Interrupted"
}

type Operation struct {
	cfg     *Config
	t       *Terminal
	buf     *RuneBuffer
	outchan chan []rune
	errchan chan error
	w       io.Writer

	*opPassword
}

type wrapWriter struct {
	r      *Operation
	t      *Terminal
	target io.Writer
}

func (w *wrapWriter) Write(b []byte) (int, error) {
	if !w.t.IsReading() {
		return w.target.Write(b)
	}

	var (
		n   int
		err error
	)
	w.r.buf.Refresh(func() {
		n, err = w.target.Write(b)
	})

	return n, err
}

func NewOperation(t *Terminal, cfg *Config) *Operation {
	width := cfg.FuncGetWidth()
	op := &Operation{
		t:       t,
		buf:     NewRuneBuffer(t, cfg.Prompt, cfg, width),
		outchan: make(chan []rune),
		errchan: make(chan error),
	}
	op.w = op.buf.w
	op.SetConfig(cfg)
	op.opPassword = newOpPassword(op)
	op.cfg.FuncOnWidthChanged(func() {
		newWidth := cfg.FuncGetWidth()
		op.buf.OnWidthChange(newWidth)
	})
	go op.ioloop()
	return op
}

func (o *Operation) SetPrompt(s string) {
	o.buf.SetPrompt(s)
}

func (o *Operation) SetMaskRune(r rune) {
	o.buf.SetMask(r)
}

func (o *Operation) ioloop() {
	for {
		r := o.t.ReadRune()
		if o.cfg.FuncFilterInputRune != nil {
			var process bool
			r, process = o.cfg.FuncFilterInputRune(r)
			if !process {
				o.buf.Refresh(nil) // to refresh the line
				continue           // ignore this rune
			}
		}

		if r == 0 { // io.EOF
			if o.buf.Len() == 0 {
				o.buf.Clean()
				select {
				case o.errchan <- io.EOF:
				}
				break
			} else {
				// if stdin got io.EOF and there is something left in buffer,
				// let's flush them by sending CharEnter.
				// And we will got io.EOF int next loop.
				r = CharEnter
			}
		}

		switch r {
		case CharTab:
			o.t.Bell()
			break
		case CharBckSearch:
			o.t.Bell()
			break
		case CharCtrlU:
			o.buf.KillFront()
		case CharFwdSearch:
			o.t.Bell()
			break
		case CharKill:
			o.buf.Kill()
		case MetaForward:
			o.buf.MoveToNextWord()
		case CharTranspose:
			o.buf.Transpose()
		case MetaBackward:
			o.buf.MoveToPrevWord()
		case MetaDelete:
			o.buf.DeleteWord()
		case CharLineStart:
			o.buf.MoveToLineStart()
		case CharLineEnd:
			o.buf.MoveToLineEnd()
		case CharBackspace, CharCtrlH:
			if o.buf.Len() == 0 {
				o.t.Bell()
				break
			}
			o.buf.Backspace()
		case CharCtrlZ:
			o.buf.Clean()
			o.t.SleepToResume()
			o.Refresh()
		case CharCtrlL:
			ClearScreen(o.w)
			o.Refresh()
		case MetaBackspace, CharCtrlW:
			o.buf.BackEscapeWord()
		case CharEnter, CharCtrlJ:
			o.buf.MoveToLineEnd()
			var data []rune
			if !o.cfg.UniqueEditLine {
				o.buf.WriteRune('\n')
				data = o.buf.Reset()
				data = data[:len(data)-1] // trim \n
			} else {
				o.buf.Clean()
				data = o.buf.Reset()
			}
			o.outchan <- data
		case CharBackward:
			o.buf.MoveBackward()
		case CharForward:
			o.buf.MoveForward()
		case CharDelete:
			if o.buf.Len() > 0 {
				o.t.KickRead()
				if !o.buf.Delete() {
					o.t.Bell()
				}
				break
			}

			// treat as EOF
			if !o.cfg.UniqueEditLine {
				o.buf.WriteString(o.cfg.EOFPrompt + "\n")
			}
			o.buf.Reset()
			o.errchan <- io.EOF
			if o.cfg.UniqueEditLine {
				o.buf.Clean()
			}
		case CharInterrupt:
			o.buf.MoveToLineEnd()
			o.buf.Refresh(nil)
			hint := o.cfg.InterruptPrompt + "\n"
			if !o.cfg.UniqueEditLine {
				o.buf.WriteString(hint)
			}
			remain := o.buf.Reset()
			if !o.cfg.UniqueEditLine {
				remain = remain[:len(remain)-len([]rune(hint))]
			}
			o.errchan <- &InterruptError{remain}
		default:
			o.buf.WriteRune(r)
		}

		if o.cfg.Listener != nil {
			newLine, newPos, ok := o.cfg.Listener.OnChange(o.buf.Runes(), o.buf.Pos(), r)
			if ok {
				o.buf.SetWithIdx(newPos, newLine)
			}
		}
	}
}

func (o *Operation) Stderr() io.Writer {
	return &wrapWriter{target: o.cfg.Stderr, r: o, t: o.t}
}

func (o *Operation) Stdout() io.Writer {
	return &wrapWriter{target: o.cfg.Stdout, r: o, t: o.t}
}

func (o *Operation) String() (string, error) {
	r, err := o.Runes()
	return string(r), err
}

func (o *Operation) Runes() ([]rune, error) {
	o.t.EnterRawMode()
	defer o.t.ExitRawMode()

	if o.cfg.Listener != nil {
		o.cfg.Listener.OnChange(nil, 0, 0)
	}

	o.buf.Refresh(nil) // print prompt
	o.t.KickRead()
	select {
	case r := <-o.outchan:
		return r, nil
	case err := <-o.errchan:
		if e, ok := err.(*InterruptError); ok {
			return e.Line, ErrInterrupt
		}
		return nil, err
	}
}

func (o *Operation) PasswordEx(prompt string, l Listener) ([]byte, error) {
	cfg := o.GenPasswordConfig()
	cfg.Prompt = prompt
	cfg.Listener = l
	return o.PasswordWithConfig(cfg)
}

func (o *Operation) GenPasswordConfig() *Config {
	return o.opPassword.PasswordConfig()
}

func (o *Operation) PasswordWithConfig(cfg *Config) ([]byte, error) {
	if err := o.opPassword.EnterPasswordMode(cfg); err != nil {
		return nil, err
	}
	defer o.opPassword.ExitPasswordMode()
	return o.Slice()
}

func (o *Operation) Password(prompt string) ([]byte, error) {
	return o.PasswordEx(prompt, nil)
}

func (o *Operation) SetTitle(t string) {
	o.w.Write([]byte("\033[2;" + t + "\007"))
}

func (o *Operation) Slice() ([]byte, error) {
	r, err := o.Runes()
	if err != nil {
		return nil, err
	}
	return []byte(string(r)), nil
}

func (op *Operation) SetConfig(cfg *Config) (*Config, error) {
	if op.cfg == cfg {
		return op.cfg, nil
	}
	if err := cfg.Init(); err != nil {
		return op.cfg, err
	}
	old := op.cfg
	op.cfg = cfg
	op.SetPrompt(cfg.Prompt)
	op.SetMaskRune(cfg.MaskRune)
	op.buf.SetConfig(cfg)

	return old, nil
}

func (o *Operation) Refresh() {
	if o.t.IsReading() {
		o.buf.Refresh(nil)
	}
}

func (o *Operation) Clean() {
	o.buf.Clean()
}

func FuncListener(f func(line []rune, pos int, key rune) (newLine []rune, newPos int, ok bool)) Listener {
	return &DumpListener{f: f}
}

type DumpListener struct {
	f func(line []rune, pos int, key rune) (newLine []rune, newPos int, ok bool)
}

func (d *DumpListener) OnChange(line []rune, pos int, key rune) (newLine []rune, newPos int, ok bool) {
	return d.f(line, pos, key)
}

type Listener interface {
	OnChange(line []rune, pos int, key rune) (newLine []rune, newPos int, ok bool)
}
