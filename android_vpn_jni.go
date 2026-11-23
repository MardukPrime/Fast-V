//go:build android && cgo

package main

/*
#include <jni.h>
*/
import "C"

import (
	"net"
	"strconv"

	"github.com/MardukPrime/Fast-V/internal/vpn"
)

// vpnManager owns the single tun dataplane the Android process can run. It lives
// here because JNI exports must be declared in package main.
var vpnManager vpn.Manager

// Java_com_wails_app_WailsBridge_nativeVpnStart hands the tun descriptor from
// FastVVpnService to the Go dataplane.
//
// The descriptor is owned by Go from this point on: the Java side detaches it
// first and never closes it again.
//
//export Java_com_wails_app_WailsBridge_nativeVpnStart
func Java_com_wails_app_WailsBridge_nativeVpnStart(env *C.JNIEnv, clazz C.jclass, fd C.jint, socksPort C.jint, mtu C.jint) C.jint {
	config := vpn.Config{
		SocksAddress: net.JoinHostPort("127.0.0.1", strconv.Itoa(int(socksPort))),
		MTU:          int(mtu),
	}
	if _, err := vpnManager.Start(int(fd), config); err != nil {
		return -1
	}
	return 0
}

// Java_com_wails_app_WailsBridge_nativeVpnStop stops the dataplane and releases
// the tun descriptor, which removes the interface.
//
//export Java_com_wails_app_WailsBridge_nativeVpnStop
func Java_com_wails_app_WailsBridge_nativeVpnStop(env *C.JNIEnv, clazz C.jclass) C.jint {
	if err := vpnManager.Stop(); err != nil {
		return -1
	}
	return 0
}
