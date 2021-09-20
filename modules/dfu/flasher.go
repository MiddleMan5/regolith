package dfu

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"time"

	"github.com/google/gousb"
)

type FlashState struct {
	step          int
	total         int
	sent          int
	bStatus       string
	bwPollTimeout int
	bState        string
	iString       string
	Complete      bool
	Error         error
}

type DeviceMetadata struct {
	Name      string
	ProductId int
}

type VendorMetadata struct {
	Name    string
	Id      int
	Devices []DeviceMetadata
}

var SupportedVendors = []VendorMetadata{
	{
		Name: "zsa",
		Id:   0x3297,
		Devices: []DeviceMetadata{
			{
				Name:      "plank",
				ProductId: 0x6060,
			}, {
				Name:      "ergodox",
				ProductId: 0x1307,
			},
			{
				Name:      "moonlander",
				ProductId: 0x1969,
			},
		},
	},
	{
		Name: "halfkay",
		Id:   0x16C0,
		Devices: []DeviceMetadata{
			{
				Name:      "halfkay",
				ProductId: 0x0478,
			},
		},
	},
	{
		Name: "dfu",
		Id:   0x0483,
		Devices: []DeviceMetadata{
			{
				Name:      "dfu",
				ProductId: 0xdf11,
			},
		},
	},
}

// Source: https://github.com/zsa/wally-cli/blob/master/usb.go
const (
	dfuSuffixVendorID  int = 0x83
	dfuSuffixProductID int = 0x11

	ergodoxMaxMemorySize = 0x100000
	ergodoxCodeSize      = 32256
	ergodoxBlockSize     = 128

	dfuSuffixLength    = 16
	planckBlockSize    = 2048
	planckStartAddress = 0x08000000
	setAddress         = 0
	eraseAddress       = 1
	eraseFlash         = 2
)

func dfuCommand(dev *gousb.Device, addr int, command int, status *FlashState) (err error) {
	var buf []byte
	if command == setAddress {
		buf = make([]byte, 5)
		buf[0] = 0x21
		buf[1] = byte(addr & 0xff)
		buf[2] = byte((addr >> 8) & 0xff)
		buf[3] = byte((addr >> 16) & 0xff)
		buf[4] = byte((addr >> 24) & 0xff)
	}
	if command == eraseAddress {
		buf = make([]byte, 5)
		buf[0] = 0x41
		buf[1] = byte(addr & 0xff)
		buf[2] = byte((addr >> 8) & 0xff)
		buf[3] = byte((addr >> 16) & 0xff)
		buf[4] = byte((addr >> 24) & 0xff)
	}
	if command == eraseFlash {
		buf = make([]byte, 1)
		buf[0] = 0x41
	}

	_, err = dev.Control(33, 1, 0, 0, buf)

	err = dfuPollTimeout(dev, status)

	if err != nil {
		return err
	}

	return nil
}

func dfuPollTimeout(dev *gousb.Device, status *FlashState) (err error) {
	for i := 0; i < 3; i++ {
		err = dfuGetStatus(dev, status)
		time.Sleep(time.Duration(status.bwPollTimeout) * time.Millisecond)
	}
	return err
}

func dfuGetStatus(dev *gousb.Device, status *FlashState) (err error) {
	buf := make([]byte, 6)
	stat, err := dev.Control(161, 3, 0, 0, buf)
	if err != nil {
		return err
	}
	if stat == 6 {
		status.bStatus = string(buf[0])
		status.bwPollTimeout = int((0xff & buf[3] << 16) | (0xff & buf[2]) | 0xff&buf[1])
		status.bState = string(buf[4])
		status.iString = string(buf[5])
	}
	return err
}

func dfuClearStatus(dev *gousb.Device) (err error) {
	_, err = dev.Control(33, 4, 2, 0, nil)
	return err
}

func dfuReboot(dev *gousb.Device, status *FlashState) (err error) {
	err = dfuPollTimeout(dev, status)
	_, err = dev.Control(33, 1, 2, 0, nil)
	time.Sleep(1000 * time.Millisecond)
	err = dfuGetStatus(dev, status)
	return err
}

func extractSuffix(fileData []byte) (hasSuffix bool, data []byte, err error) {

	fileSize := len(fileData)

	suffix := fileData[fileSize-dfuSuffixLength : fileSize]
	ext := string(suffix[10]) + string(suffix[9]) + string(suffix[8])

	// Check if image is a bootloader image
	if ext == "DFU" {
		vid := int((suffix[5] << 8) + suffix[4])
		pid := int((suffix[3] << 8) + suffix[2])
		if vid != dfuSuffixVendorID || pid != dfuSuffixProductID {
			message := fmt.Sprintf("Invalid vendor or product id, expected %#x:%#x got %#x:%#x", dfuSuffixVendorID, dfuSuffixProductID, vid, pid)
			err = errors.New(message)
			return true, fileData, err

		}

		return true, fileData[0 : fileSize-dfuSuffixLength], nil
	}

	return false, fileData, nil
}

func execFlash(firmwarePath string, deviceData *FoundDevice) FlashState {
	dfuStatus := FlashState{}
	dfuStatus.Complete = false
	fileData, err := ioutil.ReadFile(firmwarePath)
	if err != nil {
		log.Fatalf("Error while opening firmware: %s", err)
		return dfuStatus
	}

	hasSuffix, firmwareData, err := extractSuffix(fileData)
	if err != nil {
		log.Fatalf("Error while extracting DFU Suffix: %s", err)
		return dfuStatus
	}

	// TODO: This really means that a dfu binary was found, which we don't support rn
	if hasSuffix {
		log.Fatalf("Unexpected file type: %s", firmwarePath)
		return dfuStatus
	}

	// TODO: Build a custom firmware that allows us to trigger dfu mode otf
	if !deviceData.Status.isDfu {
		log.Fatalf("Device not in dfu mode: %s", deviceData.Description.String())
		return dfuStatus
	}

	// Open device
	ctx := gousb.NewContext()
	ctx.Debug(0)
	defer ctx.Close()
	device, err := ctx.OpenDeviceWithVIDPID(deviceData.Description.Vendor, deviceData.Description.Product)
	defer device.Close()

	device.SetAutoDetach(true)
	device.ControlTimeout = 5 * time.Second

	cfg, err := device.Config(1)
	if err != nil {
		log.Fatalf("Error while claiming the usb interface: %s", err)
	}
	defer cfg.Close()

	fileSize := len(firmwareData)
	dfuStatus.total = fileSize

	err = dfuClearStatus(device)
	if err != nil {
		log.Fatalf("Error while clearing the device status: %s", err)
	}

	dfuStatus.step = 1

	err = dfuCommand(device, 0, eraseFlash, &dfuStatus)
	if err != nil {
		log.Fatalf("Error while erasing flash: %s", err)
		return dfuStatus
	}

	for page := 0; page < fileSize; page += planckBlockSize {
		addr := planckStartAddress + page
		chunckSize := planckBlockSize

		if page+chunckSize > fileSize {
			chunckSize = fileSize - page
		}

		err = dfuCommand(device, addr, eraseAddress, &dfuStatus)
		if err != nil {
			log.Fatalf("Error while sending the erase address command: %s", err)
		}
		err = dfuCommand(device, addr, setAddress, &dfuStatus)
		if err != nil {
			log.Fatalf("Error while sending the set address command: %s", err)
		}

		buf := firmwareData[page : page+chunckSize]
		bytes, err := device.Control(33, 1, 2, 0, buf)

		if err != nil {
			log.Fatalf("Error while sending firmware bytes: %s", err)
		}

		dfuStatus.sent += bytes
	}

	err = dfuReboot(device, &dfuStatus)
	if err != nil {
		log.Fatalf("Error while rebooting device: %s", err)
		return dfuStatus
	}

	dfuStatus.step = 2
	dfuStatus.Complete = true
	return dfuStatus
}

func getMetdata(desc *gousb.DeviceDesc) (*DeviceMetadata, error) {
	for vI, vendor := range SupportedVendors {
		if desc.Vendor == gousb.ID(vendor.Id) {
			for dI, device := range vendor.Devices {
				if desc.Product == gousb.ID(device.ProductId) {
					return &(SupportedVendors[vI].Devices[dI]), nil
				}
			}
		}
	}
	return nil, fmt.Errorf("Failed to find metadata for device %s", desc.String())
}

func isSupported(desc *gousb.DeviceDesc) bool {
	for _, vendor := range SupportedVendors {
		if desc.Vendor == gousb.ID(vendor.Id) {
			for _, device := range vendor.Devices {
				if desc.Product == gousb.ID(device.ProductId) {
					return true
				}
			}
		}
	}
	return false
}

type DeviceStatus struct {
	isDfu bool
}

type FoundDevice struct {
	Status      *DeviceStatus
	Metadata    *DeviceMetadata
	Description *gousb.DeviceDesc
}

// Scan for compatible keyboards
func ScanDevices() []FoundDevice {
	ctx := gousb.NewContext()
	ctx.Debug(0)
	defer ctx.Close()
	// Get the list of device that match TMK's vendor id
	devs, _ := ctx.OpenDevices(isSupported)
	var devices = []FoundDevice{}
	for _, dev := range devs {

		metadata, _ := getMetdata(dev.Desc)
		devices = append(devices, FoundDevice{
			Status:      &DeviceStatus{isDfu: metadata.Name == "dfu"},
			Description: dev.Desc,
			Metadata:    metadata,
		})
	}
	for _, d := range devs {
		d.Close()
	}
	return devices
}

type DfuFlash struct {
	Firmware string
}

func (df *DfuFlash) Flash(device *FoundDevice) (string, error) {
	state := execFlash(df.Firmware, device)
	if state.Error != nil {
		return df.Firmware, state.Error
	}
	return df.Firmware, nil
}

func NewDfuFlash(firmwarePath string) *DfuFlash {
	df := &DfuFlash{
		Firmware: firmwarePath,
	}

	return df
}
