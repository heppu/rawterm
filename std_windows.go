// +build windows

package rawterm

func init() {
	Stdin = NewRawReader()
	Stdout = NewANSIWriter(Stdout)
	Stderr = NewANSIWriter(Stderr)
}
