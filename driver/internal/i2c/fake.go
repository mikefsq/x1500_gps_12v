package i2c

// Fake emulates an I2C device for tests on any platform.
//
// By default it models a register device: a Write of [reg, data...] sets the
// register pointer to reg and stores data from there; a Read returns bytes
// from the pointer onward. OnWrite, if set, runs after each Write so a test can
// react to commands (e.g. load Reply, or update a status register). If Reply is
// non-nil, Read drains it instead of the registers, which models command-based
// chips such as the SHT45.
type Fake struct {
	Regs    [256]byte
	Reply   []byte
	Writes  [][]byte
	OnWrite func(f *Fake, p []byte)
	Closed  bool
	ptr     byte
}

func (f *Fake) Write(p []byte) (int, error) {
	cp := append([]byte(nil), p...)
	f.Writes = append(f.Writes, cp)
	if len(p) >= 1 {
		f.ptr = p[0]
		for i, b := range p[1:] {
			f.Regs[byte(int(f.ptr)+i)] = b
		}
	}
	if f.OnWrite != nil {
		f.OnWrite(f, cp)
	}
	return len(p), nil
}

func (f *Fake) Read(p []byte) (int, error) {
	if f.Reply != nil {
		n := copy(p, f.Reply)
		f.Reply = f.Reply[n:]
		return n, nil
	}
	for i := range p {
		p[i] = f.Regs[byte(int(f.ptr)+i)]
	}
	return len(p), nil
}

func (f *Fake) Tx(w, r []byte) error {
	if _, err := f.Write(w); err != nil {
		return err
	}
	_, err := f.Read(r)
	return err
}

func (f *Fake) Close() error { f.Closed = true; return nil }
