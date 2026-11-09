package tell

import (
	"errors"
	"strconv"
	"strings"
)

// parseFloatPortable accepts IEEE-754 overflow and underflow results. Go's
// strconv.ParseFloat returns ErrRange together with the correct +/-Inf or
// signed-zero result; those values are part of the language semantics.
func parseFloatPortable(source string) (float64, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(source), 64)
	if err == nil {
		return value, nil
	}
	var numberError *strconv.NumError
	if errors.As(err, &numberError) && errors.Is(numberError.Err, strconv.ErrRange) {
		return value, nil
	}
	return 0, err
}
