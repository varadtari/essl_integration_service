package main

import (
	// Bundled zone data: gym timezones resolve even on an image without /usr/share/zoneinfo.
	_ "time/tzdata"

	"essl_integration_service/app"
	"essl_integration_service/utils"
)

// .env is loaded by the env package, which every package reading configuration in init() imports.

func main() {

	utils.ShowServiceInfo()

	app.StartApp()

}
