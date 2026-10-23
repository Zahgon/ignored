package evaluator_test

import (
	"fmt"
	"os"

	"github.com/ryanmab/ignored/evaluator"
)

// The mock project only counts as a repository once it has a .git entry.
func Example() {
	_ = os.Mkdir("../tests/fixtures/mock-project/.git", 0o755)
	ev := evaluator.NewEvaluator()
	fmt.Println(ev.IsIgnored("../tests/fixtures/mock-project/file.tmp"))
	// Output: true
}

func ExampleEvaluator() {
	_ = os.Mkdir("../tests/fixtures/mock-project/.git", 0o755)
	ev := evaluator.NewEvaluator()
	ignored := ev.IsIgnored("../tests/fixtures/mock-project/file.tmp")
	fmt.Println(ignored)
	// Output: true
}

func ExampleEvaluator_IsIgnored() {
	_ = os.Mkdir("../tests/fixtures/mock-project/.git", 0o755)
	ev := evaluator.NewEvaluator()
	fmt.Println(ev.IsIgnored("../tests/fixtures/mock-project/file.tmp"))
	// Output: true
}
