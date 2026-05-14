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

	result := make([]byte, len(pattern))
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			result[i] = '*'
		default:
			if i < len(s) {
				result[i] = s[i]
			} else {
				result[i] = pattern[i]
			}
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
