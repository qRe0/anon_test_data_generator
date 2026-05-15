package generator

// ---------------------------------------------------------------------------
// masking.partial
// ---------------------------------------------------------------------------

type partialMaskTransformer struct{}

func (t *partialMaskTransformer) Apply(value any, p Params) any {
	s, ok := value.(string)
	if !ok || s == "" {
		return value
	}
	pattern := p.String("pattern", "")
	if pattern == "" {
		return value
	}

	// Collect digit positions in the source value.
	type digitPos struct {
		ch byte
	}
	var digits []digitPos
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			digits = append(digits, digitPos{s[i]})
		}
	}

	// Count # in pattern for trailing alignment.
	sharpCount := 0
	for _, ch := range pattern {
		if ch == '#' {
			sharpCount++
		}
	}
	skipDigits := len(digits) - sharpCount
	if skipDigits < 0 {
		skipDigits = len(digits) // not enough digits → mask all #
	}
	sharpIdx := 0

	result := make([]byte, len(pattern))
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			result[i] = '*'
		case '#':
			dIdx := skipDigits + sharpIdx
			if dIdx < len(digits) {
				result[i] = digits[dIdx].ch
			} else {
				result[i] = '#'
			}
			sharpIdx++
		default:
			result[i] = pattern[i]
		}
	}
	return string(result)
}

// ---------------------------------------------------------------------------
// nulling
// ---------------------------------------------------------------------------

type nullingTransformer struct{}

func (t *nullingTransformer) Apply(_ any, _ Params) any {
	return nil
}
