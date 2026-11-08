//go:build windows && cgo

package audioio

/*
#cgo windows LDFLAGS: -lole32 -luuid -lpropsys
#define COBJMACROS
#define INITGUID
#include <windows.h>
#include <initguid.h>
#include <mmdeviceapi.h>
#include <endpointvolume.h>
#include <audiopolicy.h>
#include <functiondiscoverykeys_devpkey.h>
#include <propvarutil.h>
#include <stdlib.h>
#include <string.h>

static int friendly_name_matches(IMMDevice *device, const char *wanted) {
    IPropertyStore *store = NULL;
    PROPVARIANT value;
    int matched = 0;
    PropVariantInit(&value);
    if (SUCCEEDED(IMMDevice_OpenPropertyStore(device, STGM_READ, &store))) {
        if (SUCCEEDED(IPropertyStore_GetValue(store, &PKEY_Device_FriendlyName, &value)) &&
            value.vt == VT_LPWSTR && value.pwszVal != NULL) {
            int needed = WideCharToMultiByte(CP_UTF8, 0, value.pwszVal, -1, NULL, 0, NULL, NULL);
            if (needed > 0) {
                char *name = (char*)malloc((size_t)needed);
                if (name != NULL) {
                    WideCharToMultiByte(CP_UTF8, 0, value.pwszVal, -1, name, needed, NULL, NULL);
                    matched = strcmp(name, wanted) == 0;
                    free(name);
                }
            }
        }
        PropVariantClear(&value);
        IPropertyStore_Release(store);
    }
    return matched;
}

static HRESULT default_playback_endpoint_name(char *destination, int capacity) {
    HRESULT init = CoInitializeEx(NULL, COINIT_MULTITHREADED);
    int uninitialize = SUCCEEDED(init);
    if (FAILED(init) && init != RPC_E_CHANGED_MODE) return init;
    IMMDeviceEnumerator *enumerator = NULL;
    IMMDevice *device = NULL;
    IPropertyStore *store = NULL;
    PROPVARIANT value;
    PropVariantInit(&value);
    HRESULT hr = CoCreateInstance(&CLSID_MMDeviceEnumerator, NULL, CLSCTX_ALL,
                                  &IID_IMMDeviceEnumerator, (void**)&enumerator);
    if (SUCCEEDED(hr)) hr = IMMDeviceEnumerator_GetDefaultAudioEndpoint(enumerator, eRender, eMultimedia, &device);
    if (SUCCEEDED(hr)) hr = IMMDevice_OpenPropertyStore(device, STGM_READ, &store);
    if (SUCCEEDED(hr)) hr = IPropertyStore_GetValue(store, &PKEY_Device_FriendlyName, &value);
    if (SUCCEEDED(hr) && value.vt == VT_LPWSTR && value.pwszVal != NULL) {
        if (WideCharToMultiByte(CP_UTF8, 0, value.pwszVal, -1, destination, capacity, NULL, NULL) == 0) {
            hr = HRESULT_FROM_WIN32(GetLastError());
        }
    } else if (SUCCEEDED(hr)) {
        hr = E_FAIL;
    }
    PropVariantClear(&value);
    if (store != NULL) IPropertyStore_Release(store);
    if (device != NULL) IMMDevice_Release(device);
    if (enumerator != NULL) IMMDeviceEnumerator_Release(enumerator);
    if (uninitialize) CoUninitialize();
    return hr;
}

static HRESULT inspect_playback_endpoint(const char *wanted, float *currentVolume, int *isMuted) {
    HRESULT init = CoInitializeEx(NULL, COINIT_MULTITHREADED);
    int uninitialize = SUCCEEDED(init);
    if (FAILED(init) && init != RPC_E_CHANGED_MODE) return init;

    IMMDeviceEnumerator *enumerator = NULL;
    IMMDeviceCollection *collection = NULL;
    IMMDevice *matched = NULL;
    HRESULT hr = CoCreateInstance(&CLSID_MMDeviceEnumerator, NULL, CLSCTX_ALL,
                                  &IID_IMMDeviceEnumerator, (void**)&enumerator);
    if (SUCCEEDED(hr)) hr = IMMDeviceEnumerator_EnumAudioEndpoints(enumerator, eRender, DEVICE_STATE_ACTIVE, &collection);
    if (SUCCEEDED(hr)) {
        UINT count = 0;
        IMMDeviceCollection_GetCount(collection, &count);
        hr = HRESULT_FROM_WIN32(ERROR_NOT_FOUND);
        for (UINT i = 0; i < count; i++) {
            IMMDevice *device = NULL;
            if (SUCCEEDED(IMMDeviceCollection_Item(collection, i, &device))) {
                if (friendly_name_matches(device, wanted)) {
                    matched = device;
                    hr = S_OK;
                    break;
                }
                IMMDevice_Release(device);
            }
        }
    }
    if (SUCCEEDED(hr) && matched != NULL) {
        IAudioEndpointVolume *volume = NULL;
        hr = IMMDevice_Activate(matched, &IID_IAudioEndpointVolume, CLSCTX_ALL, NULL, (void**)&volume);
        if (SUCCEEDED(hr)) {
            float current = 1.0f;
            BOOL muted = FALSE;
            IAudioEndpointVolume_GetMasterVolumeLevelScalar(volume, &current);
            IAudioEndpointVolume_GetMute(volume, &muted);
            if (currentVolume != NULL) *currentVolume = current;
            if (isMuted != NULL) *isMuted = muted ? 1 : 0;
            IAudioEndpointVolume_Release(volume);
        }
    }

    if (matched != NULL) IMMDevice_Release(matched);
    if (collection != NULL) IMMDeviceCollection_Release(collection);
    if (enumerator != NULL) IMMDeviceEnumerator_Release(enumerator);
    if (uninitialize) CoUninitialize();
    return hr;
}

static HRESULT prepare_playback_endpoint(const char *wanted) {
    HRESULT init = CoInitializeEx(NULL, COINIT_MULTITHREADED);
    int uninitialize = SUCCEEDED(init);
    if (FAILED(init) && init != RPC_E_CHANGED_MODE) return init;
    IMMDeviceEnumerator *enumerator = NULL;
    IMMDeviceCollection *collection = NULL;
    IMMDevice *matched = NULL;
    HRESULT hr = CoCreateInstance(&CLSID_MMDeviceEnumerator, NULL, CLSCTX_ALL,
                                  &IID_IMMDeviceEnumerator, (void**)&enumerator);
    if (SUCCEEDED(hr)) hr = IMMDeviceEnumerator_EnumAudioEndpoints(enumerator, eRender, DEVICE_STATE_ACTIVE, &collection);
    if (SUCCEEDED(hr)) {
        UINT count = 0;
        IMMDeviceCollection_GetCount(collection, &count);
        hr = HRESULT_FROM_WIN32(ERROR_NOT_FOUND);
        for (UINT i = 0; i < count; i++) {
            IMMDevice *device = NULL;
            if (SUCCEEDED(IMMDeviceCollection_Item(collection, i, &device))) {
                if (friendly_name_matches(device, wanted)) {
                    matched = device;
                    hr = S_OK;
                    break;
                }
                IMMDevice_Release(device);
            }
        }
    }
    if (SUCCEEDED(hr) && matched != NULL) {
        IAudioEndpointVolume *volume = NULL;
        hr = IMMDevice_Activate(matched, &IID_IAudioEndpointVolume, CLSCTX_ALL, NULL, (void**)&volume);
        if (SUCCEEDED(hr)) hr = IAudioEndpointVolume_SetMute(volume, FALSE, NULL);
        if (SUCCEEDED(hr)) hr = IAudioEndpointVolume_SetMasterVolumeLevelScalar(volume, 1.0f, NULL);
        if (volume != NULL) IAudioEndpointVolume_Release(volume);
    }
    if (matched != NULL) IMMDevice_Release(matched);
    if (collection != NULL) IMMDeviceCollection_Release(collection);
    if (enumerator != NULL) IMMDeviceEnumerator_Release(enumerator);
    if (uninitialize) CoUninitialize();
    return hr;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

type PlaybackVolumeStatus struct {
	Percent float64
	Muted   bool
}

// InspectPlaybackEndpoint reports the current Windows endpoint state without
// changing its volume, mute flag, or any per-application audio session.
func InspectPlaybackEndpoint(name string) (PlaybackVolumeStatus, error) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	var previous C.float
	var muted C.int
	hr := C.inspect_playback_endpoint(cName, &previous, &muted)
	status := PlaybackVolumeStatus{Percent: float64(previous) * 100, Muted: muted != 0}
	if hr < 0 {
		return status, fmt.Errorf("consultar volumen de Windows para %q: HRESULT 0x%08X", name, uint32(hr))
	}
	return status, nil
}

// PreparePlaybackEndpoint ensures the dedicated radio playback endpoint cannot
// silently attenuate the stream before it reaches the transceiver.
func PreparePlaybackEndpoint(name string) error {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	if hr := C.prepare_playback_endpoint(cName); hr < 0 {
		return fmt.Errorf("prepare Windows playback endpoint %q: HRESULT 0x%08X", name, uint32(hr))
	}
	return nil
}

func DefaultPlaybackEndpointName() (string, error) {
	const capacity = 1024
	buffer := C.malloc(capacity)
	if buffer == nil {
		return "", fmt.Errorf("allocate memory for the default device")
	}
	defer C.free(buffer)
	hr := C.default_playback_endpoint_name((*C.char)(buffer), capacity)
	if hr < 0 {
		return "", fmt.Errorf("query Windows default output: HRESULT 0x%08X", uint32(hr))
	}
	return C.GoString((*C.char)(buffer)), nil
}
