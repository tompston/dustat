package main

func main() {
	v := UsedStruct{}
	_ = v.A
}

type UsedStruct struct {
	A string
}

type UnusedStruct struct {
	A string
}

type UnusedButIgnoredStruct struct {
	A string
}

const MY_CONS = "constant"
