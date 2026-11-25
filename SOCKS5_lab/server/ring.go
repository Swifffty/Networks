package server

type ring struct {
	buf  []byte
	head int
	size int
}

func newRing(n int) *ring {
	return &ring{buf: make([]byte, n)}
}

func (r *ring) free() int {
	return len(r.buf) - r.size
}

func (r *ring) readSeg() []byte {
	end := r.head + r.size
	if end > len(r.buf) {
		end = len(r.buf)
	}
	return r.buf[r.head:end]
}

func (r *ring) writeSeg() []byte {
	if r.size == len(r.buf) {
		return nil
	}
	tail := r.head + r.size
	if tail >= len(r.buf) {
		tail -= len(r.buf)
		return r.buf[tail:r.head]
	}
	return r.buf[tail:]
}

func (r *ring) produce(n int) {
	r.size += n
}

func (r *ring) consume(n int) {
	r.head += n
	if r.head >= len(r.buf) {
		r.head -= len(r.buf)
	}
	r.size -= n
	if r.size == 0 {
		r.head = 0
	}
}

func (r *ring) peek(dst []byte) int {
	n := copy(dst, r.readSeg())
	if n < len(dst) && n < r.size {
		n += copy(dst[n:], r.buf[:r.size-n])
	}
	return n
}

func (r *ring) put(p []byte) bool {
	if len(p) > r.free() {
		return false
	}
	for len(p) > 0 {
		n := copy(r.writeSeg(), p)
		r.size += n
		p = p[n:]
	}
	return true
}
