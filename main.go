package main

import (
	"essl_integration_service/app"
	"essl_integration_service/utils"
)

//please leave this space so that utils package gets imported first. This is needed so that env variables get loaded first!

func main() {

	utils.ShowServiceInfo()

	app.StartApp()

}
