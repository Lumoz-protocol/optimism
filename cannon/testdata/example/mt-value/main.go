package main

import (
	"fmt"

	"utils/testutil"
)

func main() {

	// TODO(#12162) Debug commented out tests which are currently failing
	testutil.RunTest(TestValue, "TestValue")
	testutil.RunTest(TestValueLarge, "TestValueLarge")
	testutil.RunTest(TestValuePanic, "TestValuePanic")
	testutil.RunTest(TestValueConcurrent, "TestValueConcurrent")
	//testutil.RunTest(TestValue_Swap, "TestValue_Swap")
	testutil.RunTest(TestValueSwapConcurrent, "TestValueSwapConcurrent")
	//testutil.RunTest(TestValue_CompareAndSwap, "TestValue_CompareAndSwap")
	testutil.RunTest(TestValueCompareAndSwapConcurrent, "TestValueCompareAndSwapConcurrent")

	fmt.Println("Value tests passed")
}
