package utils

func ExitFromErr(err error) {
	if err != nil {
		panic(err)
	}
}
