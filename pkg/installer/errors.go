package installer

import "fmt"

type VersionMismatchError struct {
	Tool, Expected, Got string
}

func (e *VersionMismatchError) Error() string {
	return fmt.Sprintf("%s: version mismatch — installed %s, expected %s", e.Tool, e.Got, e.Expected)
}
