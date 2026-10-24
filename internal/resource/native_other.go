//go:build !linux && !darwin && !windows

package resource

import "os"

func nativeValidateAnchor(string) error                             { return errNativePolicy }
func nativeOpenAnchor(string) (*os.File, error)                     { return nil, errNativePolicy }
func nativeOpenChild(*os.File, string, bool) (*os.File, error)      { return nil, errNativePolicy }
func nativeOpenMetadata(*os.File, string, bool) (*os.File, error)   { return nil, errNativePolicy }
func nativeOpenGitMetadata(*os.File) (*os.File, error)              { return nil, errNativePolicy }
func nativeOpenGitAliasMetadata(*os.File, string) (*os.File, error) { return nil, errNativePolicy }
func nativeIdentity(*os.File) (string, error)                       { return "", errNativePolicy }
func nativeRepresentable(string) bool                               { return false }
func nativePolicyError(error) bool                                  { return true }
func nativeStatAt(*os.File, string) (filesystemMetadata, error) {
	return filesystemMetadata{}, errNativePolicy
}
