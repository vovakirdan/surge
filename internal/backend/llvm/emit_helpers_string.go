package llvm

import "fmt"

func (fe *funcEmitter) emitStringConcat(left, right string) string {
	leftAddr := fe.emitHandleAddr(left)
	rightAddr := fe.emitHandleAddr(right)
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_string_concat(ptr %s, ptr %s)\n", tmp, leftAddr, rightAddr)
	return tmp
}

func (fe *funcEmitter) emitStringConcatAll(parts ...string) (string, error) {
	if len(parts) == 0 {
		return "", fmt.Errorf("string concat requires at least one part")
	}
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		out = fe.emitStringConcat(out, parts[i])
	}
	return out, nil
}
