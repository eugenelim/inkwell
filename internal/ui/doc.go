// Package ui hosts the Bubble Tea models, commands, and theme. See spec
// 04 and ARCH §10. The UI never blocks on I/O: every Graph or DB call is
// dispatched as a tea.Cmd that emits a typed tea.Msg on completion.
package ui
