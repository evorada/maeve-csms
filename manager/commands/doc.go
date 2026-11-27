// SPDX-License-Identifier: Apache-2.0

// Package commands queues the commands of the CSMS to the charge stations and sends them.
//
// A command is stored as queued (Service.Enqueue) and sent by the Dispatcher when its
// station is connected, one command per station at a time, with the command id as the
// OCPP MessageId. The station echoes that id in its CallResult or CallError, so the
// router hands every answer to the Dispatcher first (handlers.CallOutcomeObserver),
// which records it on the command and sends the next one. Answers to the calls that
// other parts of the manager send (with their own MessageIds) are not commands and are
// left alone. A sweep, run as a background job, times out the commands that expire in
// the queue or get no answer in time and retries the sends that failed.
//
// Every final status (accepted, rejected, failed, timeout, cancelled) is stored in one
// store transaction with its command.completed event. The actions that can be sent are
// those of the catalogue (Lookup), by OCPP version.
package commands
