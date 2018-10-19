package chain

import (
	"fmt"
	"github.com/copernet/copernicus/model/blockindex"
)

// NotificationType represents the type of a notification message.
type NotificationType int

// NotificationCallback is used for a caller to provide a callback for
// notifications about various chain events.
type NotificationCallback func(*Notification)

// Constants for the type of a notification message.
const (
	// NTBlockAccepted indicates the associated block was accepted into
	// the block chain.  Note that this does not necessarily mean it was
	// added to the main chain.  For that, use NTBlockConnected.
	NTBlockAccepted NotificationType = iota

	// NTBlockConnected indicates the associated block was connected to the
	// main chain.
	NTBlockConnected

	// NTBlockDisconnected indicates the associated block was disconnected
	// from the main chain.
	NTBlockDisconnected

	// NTChainTipUpdated indicates the associated blocks leads to the new main chain.
	NTChainTipUpdated
)

// notificationTypeStrings is a map of notification types back to their constant
// names for pretty printing.
var notificationTypeStrings = map[NotificationType]string{
	NTBlockAccepted:     "NTBlockAccepted",
	NTBlockConnected:    "NTBlockConnected",
	NTBlockDisconnected: "NTBlockDisconnected",
}

// String returns the NotificationType in human-readable form.
func (n NotificationType) String() string {
	if s, ok := notificationTypeStrings[n]; ok {
		return s
	}
	return fmt.Sprintf("Unknown Notification Type (%d)", int(n))
}

type TipUpdatedEvent struct {
	TipIndex          *blockindex.BlockIndex
	ForkIndex         *blockindex.BlockIndex
	IsInitialDownload bool
}

// Notification defines notification that is sent to the caller via the callback
// function provided during the call to New and consists of a notification type
// as well as associated data that depends on the type as follows:
// 	- NTBlockAccepted:     *btcutil.Block
// 	- NTBlockConnected:    *btcutil.Block
// 	- NTBlockDisconnected: *btcutil.Block
type Notification struct {
	Type NotificationType
	Data interface{}
}

// Subscribe to block chain notifications. Registers a callback to be executed
// when various events take place. See the documentation on Notification and
// NotificationType for details on the types and contents of notifications.
func (c *Chain) Subscribe(callback NotificationCallback) {
	c.notificationsLock.Lock()
	c.notifications = append(c.notifications, callback)
	c.notificationsLock.Unlock()
}

// SendNotification sends a notification with the passed type and data if the
// caller requested notifications by providing a callback function in the call
// to New.
func (c *Chain) SendNotification(typ NotificationType, data interface{}) {
	n := Notification{Type: typ, Data: data}

	c.notificationsLock.RLock()
	defer c.notificationsLock.RUnlock()

	for _, callback := range c.notifications {
		callback(&n)
	}
}
