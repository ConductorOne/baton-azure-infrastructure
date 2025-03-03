package connector

// Convert accepts a list of T and returns a list of R based on the input func.
func Convert[T any, R comparable](slice []T, f func(in T) R) []R {
	var nilR R
	ret := make([]R, 0, len(slice))
	for _, t := range slice {
		r := f(t)
		if r != nilR {
			ret = append(ret, r)
		}
	}
	return ret
}

// ConvertErr is a variant of Convert that also escapes if there's an error converting the item T into R.
func ConvertErr[T any, R comparable](slice []T, f func(in T) (R, error)) ([]R, error) {
	var nilR R
	ret := make([]R, 0, len(slice))
	for _, t := range slice {
		x, err := f(t)
		if err != nil {
			return nil, err
		}
		if x != nilR {
			ret = append(ret, x)
		}
	}
	return ret, nil
}
