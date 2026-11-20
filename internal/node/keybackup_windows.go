//go:build windows

package node

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// On Windows the backup is a generic credential in Credential Manager:
// protected by the user's account, kept on this computer (not roamed),
// and outside the settings folder. Plain advapi32 calls, no cgo.

var (
	advapi32       = windows.NewLazySystemDLL("advapi32.dll")
	procCredWriteW = advapi32.NewProc("CredWriteW")
	procCredReadW  = advapi32.NewProc("CredReadW")
	procCredDelete = advapi32.NewProc("CredDeleteW")
	procCredFree   = advapi32.NewProc("CredFree")
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
	errorNotFound           = windows.Errno(1168) // ERROR_NOT_FOUND
	maxCredentialBlobSize   = 5 * 512
)

// credential mirrors CREDENTIALW.
type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

func credTarget(dataDir string) (*uint16, error) {
	return windows.UTF16PtrFromString("Link Node identity " + backupName(dataDir))
}

func loadKeyBackup(dataDir string) ([]byte, error) {
	target, err := credTarget(dataDir)
	if err != nil {
		return nil, err
	}
	var c *credential
	r, _, callErr := procCredReadW.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&c)))
	if r == 0 {
		return nil, callErr
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(c))) //nolint:errcheck
	if c.CredentialBlobSize == 0 || c.CredentialBlob == nil {
		return nil, errors.New("empty credential")
	}
	return append([]byte(nil), unsafe.Slice(c.CredentialBlob, c.CredentialBlobSize)...), nil
}

func writeKeyBackup(dataDir string, data []byte) error {
	if len(data) == 0 || len(data) > maxCredentialBlobSize {
		return errors.New("key backup has an invalid size")
	}
	target, err := credTarget(dataDir)
	if err != nil {
		return err
	}
	user, _ := windows.UTF16PtrFromString("Link Node")
	comment, _ := windows.UTF16PtrFromString("Link Node's identity key. Deleting it loses the node's address if its settings folder is lost too.")
	c := credential{
		Type:               credTypeGeneric,
		TargetName:         target,
		Comment:            comment,
		CredentialBlobSize: uint32(len(data)),
		CredentialBlob:     &data[0],
		Persist:            credPersistLocalMachine,
		UserName:           user,
	}
	if r, _, callErr := procCredWriteW.Call(uintptr(unsafe.Pointer(&c)), 0); r == 0 {
		return callErr
	}
	return nil
}

func deleteKeyBackup(dataDir string) error {
	target, err := credTarget(dataDir)
	if err != nil {
		return err
	}
	if r, _, callErr := procCredDelete.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0); r == 0 && !errors.Is(callErr, errorNotFound) {
		return callErr
	}
	return nil
}
