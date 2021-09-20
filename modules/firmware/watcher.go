package firmware

import (
	"path"

	"github.com/howeyc/fsnotify"
)

type FirmwareWatcher struct {
	Error   chan error
	Event   chan *FirmwareWatchEvent
	Watcher *fsnotify.Watcher
}

type FirmwareWatchEvent struct {
	Name string
}

// TODO: Better validation
func isFirmwareFile(filePath string) bool {
	ext := path.Ext(filePath)
	return ext == ".hex" || ext == ".bin"
}

func (fw *FirmwareWatcher) watch(watchPath string) error {
	go func() {
		for {
			select {
			case event := <-fw.Watcher.Event:
				var firmwarePath = event.Name
				if isFirmwareFile(firmwarePath) {
					if event.IsModify() || event.IsCreate() {
						fw.Event <- &FirmwareWatchEvent{Name: event.Name}
					}
				}
			case err := <-fw.Watcher.Error:
				fw.Error <- err
			}
		}
	}()

	err := fw.Watcher.Watch(watchPath)
	if err != nil {
		return err
	}

	return nil
}

func (fw *FirmwareWatcher) Close() {
	fw.Watcher.Close()
}

func NewFirmwareWatcher(watchPath string) (*FirmwareWatcher, error) {
	errChan := make(chan error)
	eventChan := make(chan *FirmwareWatchEvent)
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	fw := &FirmwareWatcher{
		Error:   errChan,
		Event:   eventChan,
		Watcher: w,
	}

	err = fw.watch(watchPath)
	if err != nil {
		return nil, err
	}

	return fw, err
}
