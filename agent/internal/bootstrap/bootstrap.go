package bootstrap

import (
	"fmt"
	"os"
)

func Run() error {
	_, err := fmt.Fprintf(os.Stdout, "SyncWin agent initialized\n")
	return err
}
