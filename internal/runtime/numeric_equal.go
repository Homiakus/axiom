package runtime

func numbersEqual(left any, right any) (bool, bool) {
	if a, ok := signedInteger(left); ok {
		if b, ok := signedInteger(right); ok {
			return a == b, true
		}
	}
	a, aok := number(left)
	b, bok := number(right)
	if !aok || !bok {
		return false, false
	}
	return a == b, true
}
