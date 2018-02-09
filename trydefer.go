package main

import (
	"fmt"
	"github.com/pkg/errors"
)

func main() {
 tryabc()
/*var 	strflot interface{}
 strflot=float64(1222)
fid:=	strflot.(float64)
fmt.Println()*/
}

func tryabc() (err error)  {
	id := createdir()
	defer func() {
		if recover()!=nil || err!=nil{
			fmt.Println("seemes to be some problem reverting ",id)
			deletedir(id)
		}
	}()
	idx := createe()
	return errors.New("sdfsdfsdf")
	defer func() {
		if(err==nil){
			fmt.Println("is nisdl",idx)
		}
	}()

	return

}

func deletedir(id int)()  {
	fmt.Println("delete dir ",id)

}
func createdir()(int)  {
	return 11

}
func createe()(int)  {
	return 22

}
