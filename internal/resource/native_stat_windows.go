package resource

import (
	"errors"
	"os"
)

func nativeStatAt(parent *os.File, name string) (filesystemMetadata, error) {
	file, err := nativeOpenMetadata(parent, name, false)
	if err != nil {
		return filesystemMetadata{}, err
	}
	info, statErr := file.Stat()
	identity, idErr := nativeIdentity(file)
	closeErr := file.Close()
	return filesystemMetadata{info: info, identity: identity}, errors.Join(statErr, idErr, closeErr)
}
