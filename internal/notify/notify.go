// Package notify posts desktop notifications through the freedesktop D-Bus
// org.freedesktop.Notifications interface, the one every Linux desktop
// environment implements (GNOME Shell, KDE Plasma, XFCE, Cinnamon, ...).
// This is the same API the notify-send CLI talks to, implemented in pure Go
// over github.com/godbus/dbus/v5 so omc never shells out to an external
// binary.
//
// Everything here is runtime-detected and best-effort: when there is no
// session bus, no notification daemon, or the daemon misbehaves, Detect
// returns nil and the caller simply skips desktop notifications. A hung
// daemon can never stall the pipeline either: every D-Bus round trip is
// bounded by a short deadline.
package notify

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// Notifier posts and closes desktop notifications. All methods are
// best-effort: failures are swallowed, and Close on an unknown id is a
// harmless no-op on the daemon side.
type Notifier interface {
	// Notify shows a notification and returns its id, or 0 on failure.
	// expire bounds its on-screen lifetime; it may be shortened by the
	// daemon.
	Notify(summary, body, icon string, expire time.Duration) uint32
	// Close removes the notification with the given id (if any).
	Close(id uint32)
	// Describe names the detected backend for diagnostics, e.g.
	// "org.freedesktop.Notifications (GNOME)".
	Describe() string
}

const (
	notificationsIface = "org.freedesktop.Notifications"
	notificationsPath  = dbus.ObjectPath("/org/freedesktop/Notifications")
	// callTimeout bounds every D-Bus round trip so a hung notification
	// daemon can never block the caller for more than a moment.
	callTimeout = 2 * time.Second
)

// Detect returns a session-bus backed Notifier when the runtime environment
// has a desktop notification daemon within reach: a D-Bus session bus (from
// DBUS_SESSION_BUS_ADDRESS or the XDG runtime dir socket) plus a process
// owning the org.freedesktop.Notifications name (the desktop's notification
// server). It returns nil otherwise, and never spawns a bus (no autolaunch).
//
// mode is the OMC_TOUCH_NOTIFY setting: "auto" (the default) performs the
// full detection including the daemon probe, "on" skips the daemon probe and
// uses the bus unconditionally (for exotic servers that do not own the
// well-known name), and "off" disables desktop notifications outright.
// An unrecognized value degrades to "auto".
func Detect(mode string) Notifier {
	if NormalizeMode(mode) == "off" {
		return nil
	}
	conn, err := dbus.SessionBusPrivateNoAutoStartup()
	if err != nil {
		return nil
	}
	if err := conn.Auth(nil); err != nil {
		conn.Close()
		return nil
	}
	if err := conn.Hello(); err != nil {
		conn.Close()
		return nil
	}

	desc := "org.freedesktop.Notifications"
	if desktop := Desktop(); desktop != "" {
		desc += " (" + desktop + ")"
	}

	if NormalizeMode(mode) == "auto" && !daemonRunning(conn) {
		conn.Close()
		return nil
	}
	return &dbusNotifier{conn: conn, desc: desc}
}

// daemonRunning reports whether some process owns the
// org.freedesktop.Notifications well-known name on the bus, i.e. whether a
// desktop notification server is actually listening.
func daemonRunning(conn *dbus.Conn) bool {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	bus := conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	call := bus.CallWithContext(ctx, "org.freedesktop.DBus.NameHasOwner", 0, notificationsIface)
	if call.Err != nil {
		return false
	}
	for _, v := range call.Body {
		if owns, ok := v.(bool); ok && owns {
			return true
		}
	}
	return false
}

// dbusNotifier implements Notifier over a private session-bus connection.
type dbusNotifier struct {
	conn *dbus.Conn
	desc string
}

// Notify implements Notifier. The notification is marked transient (where
// supported) so ephemeral touch prompts do not pile up in the notification
// center history, and the desktop-entry hint lets GNOME & co. attribute it
// to omc.
func (n *dbusNotifier) Notify(summary, body, icon string, expire time.Duration) uint32 {
	if n == nil || n.conn == nil {
		return 0
	}
	hints := map[string]dbus.Variant{
		"urgency":       dbus.MakeVariant(byte(1)), // normal
		"transient":     dbus.MakeVariant(true),
		"desktop-entry": dbus.MakeVariant("omc"),
	}
	// Per the spec: 0 = never expire, -1 = daemon default. Our callers
	// always pass a positive budget; keep a sane fallback for zero.
	ms := int32(expire / time.Millisecond)
	if ms <= 0 {
		ms = -1
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	obj := n.conn.Object(notificationsIface, notificationsPath)
	call := obj.CallWithContext(ctx, notificationsIface+".Notify", 0,
		"omc", uint32(0), icon, summary, body, []string(nil), hints, ms)
	if call.Err != nil || len(call.Body) == 0 {
		return 0
	}
	id, ok := call.Body[0].(uint32)
	if !ok {
		return 0
	}
	return id
}

// Close implements Notifier.
func (n *dbusNotifier) Close(id uint32) {
	if n == nil || n.conn == nil || id == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	obj := n.conn.Object(notificationsIface, notificationsPath)
	obj.CallWithContext(ctx, notificationsIface+".CloseNotification", 0, id)
}

// Describe implements Notifier.
func (n *dbusNotifier) Describe() string {
	if n == nil {
		return ""
	}
	return n.desc
}

// NormalizeMode maps a raw OMC_TOUCH_NOTIFY value to "auto", "on", or
// "off". The empty string and unrecognized values degrade to "auto".
func NormalizeMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "on", "1", "true", "yes", "force":
		return "on"
	case "off", "0", "false", "no", "disable", "disabled":
		return "off"
	default:
		return "auto"
	}
}

// Desktop returns the first desktop environment name from
// XDG_CURRENT_DESKTOP (e.g. "GNOME" from "GNOME:ubuntu:wayland"), or "" when
// unset. It is purely informational, used for diagnostics: the notification
// channel itself is D-Bus, which works across every desktop implementing
// the freedesktop notification spec.
func Desktop() string {
	v := strings.TrimSpace(os.Getenv("XDG_CURRENT_DESKTOP"))
	if v == "" {
		return ""
	}
	if i := strings.IndexByte(v, ':'); i >= 0 {
		v = v[:i]
	}
	return v
}
