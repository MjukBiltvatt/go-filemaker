package filemaker

import (
	"fmt"
	"os"
	"testing"
)

func TestNew(t *testing.T) {
	sess, err := New(os.Getenv("HOST"), os.Getenv("DATABASE"), os.Getenv("USERNAME"), os.Getenv("PASSWORD"))
	if err != nil {
		fmt.Println("Error:", err.Error())
		return
	}
	defer sess.Destroy()
}
