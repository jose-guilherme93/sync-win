package bootstrap

import (
	"fmt"
	"os"
)

func Run() error {
	_, err := fmt.Fprintf(os.Stdout, "LEM agent initialized\n")
	return err
}
