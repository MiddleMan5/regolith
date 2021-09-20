package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/MiddleMan5/regolith/modules/dfu"
	"github.com/MiddleMan5/regolith/modules/firmware"
	"github.com/MiddleMan5/regolith/modules/util"
	"github.com/urfave/cli"
)

// App Name and Version
const (
	AppName      = "regolith"
	AppVer       = "0.0.1"
	DefaultWatch = "~/Downloads/fw"
)

// Reset command empties configuration folders to leave a clean slate
func resetAction(ctx *cli.Context) error {
	return nil
}

// Setup initializes the configuration directory and binds keyboards
func setupAction(ctx *cli.Context) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Printf("Enter download directory to watch for new firmware (default: %s): ", DefaultWatch)
	downloadDirectory, _ := reader.ReadString('\n')

	// fmt.Println("Settings saved to ~/.ergogo/settings.json")

	// fmt.Println("Your setup looks good! Run `ergogo run` to start using or `ergogo reset` to clear settings. :)")

	fmt.Println(downloadDirectory)
	return nil
}

var exit = make(chan bool)

func flashFirmware(firmwarePath string) {
	log.Printf("Flashing firmware: %s", firmwarePath)
	df := dfu.NewDfuFlash(firmwarePath)
	// TODO: Automatically start and stop watchers for devices as they come and go

	devices := dfu.ScanDevices()
	if len(devices) > 0 {
		for _, dev := range devices {
			var deviceName = dev.Metadata.Name
			log.Printf("Flashing device %s with %s", deviceName, firmwarePath)
			file, err := df.Flash(&dev)
			if err == nil {
				log.Printf("Successfully flashed device %s with %s", deviceName, file)
			} else {
				log.Fatalf("Failed to flash device %s: %v", deviceName, err)
				panic(err)
			}
		}
	} else {
		log.Printf("No compatible devices found")
	}

}

func startupFirmwareWatcher(firmwareDirectory string) {
	firmwareDirectory = util.ExpandPath(firmwareDirectory)
	log.Printf("Watching directory '%s'", firmwareDirectory)
	fw, err := firmware.NewFirmwareWatcher(firmwareDirectory)
	if err != nil {
		log.Fatalf("Error starting firmware watcher: %v", err)
	}

	go func() {
		var flashing = false
		for {
			select {
			case event := <-fw.Event:
				log.Printf("Detected firmware event %s", event.Name)
				if !flashing {
					flashing = true
					flashFirmware(event.Name)
					flashing = false
				} else {
					log.Println("Ignoring event; flashing already in progress")
				}
			case err := <-fw.Error:
				log.Fatalf("Unhandled error in firmware watcher: %v", err)
				fw.Close()
			case <-exit:
				fw.Close()
			}

		}
	}()
}

// CmdRun is a placeholder default command and runs the GUI
func runApp(ctx *cli.Context) error {

	log.Println("Waiting")

	go startupFirmwareWatcher(DefaultWatch)

	// Block until exit channel receives input
	<-exit
	log.Println("Stopping")

	return nil
}

func main() {

	app := cli.NewApp()
	app.Name = AppName
	app.Usage = "Automatically flash ergodox firmware"
	app.Version = AppVer
	app.Commands = []cli.Command{
		{
			Name:   "run",
			Usage:  "Starts Ergogo main application (default)",
			Action: runApp,
		},
	}
	app.Flags = append(app.Flags, []cli.Flag{}...)
	log.Println("Running")
	app.Run(os.Args)

	// Capture sigint
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		os.Exit(1)
	}()
}
