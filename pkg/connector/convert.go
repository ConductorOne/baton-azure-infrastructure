package connector

// ConvertErr is a variant of Convert that also escapes if there's an error converting the item T into R.
func ConvertErr[T any, R comparable](slice []T, f func(in T) (R, error)) ([]R, error) {
	// TODO: convert to user iter.MapErr
	// Needs to verify any concurrency issue
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
