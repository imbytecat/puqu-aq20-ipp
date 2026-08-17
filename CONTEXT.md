# Domain glossary

## Print server

One deployed instance that manages physical devices and serves one or more direct-addressed IPP queues.

## Driver

A named capability contract for one printer family. A driver determines supported transports, document formats, media rules, and device-specific printing behavior. `puqu-aq20` is the only supported driver today.

## Device

A discovered or saved physical endpoint. A device describes how the print server reaches hardware; it is not independently visible to operating-system print clients.

## Printer

A configured logical IPP queue with a stable path and UUID. Each printer selects one driver, may attach one device, and selects one label profile. A physical device belongs to at most one printer.

## Label profile

Reusable label stock dimensions and optional host-side raster tuning. A printer selects one profile; a profile may be shared by multiple printers.

## Job

One document submission to exactly one printer. Jobs never move between printers. Different printers may process jobs concurrently; jobs within one printer remain ordered.
