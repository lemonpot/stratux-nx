<div align="center">

# Stratux NX

**A modern, independent Stratux-based distribution by [Lemonpot](https://lemonpot.ai/)**

Enhanced flight logging, managed internet connectivity, a modern interface, offline aviation data, and next-generation attitude estimation.

</div>

## Overview

Stratux NX is an independent distribution based on the open-source [Stratux](https://github.com/cyoung/stratux) project. It builds on Stratux's proven foundation while adding an integrated set of features focused on everyday cockpit use, post-flight records, connectivity control, and a refreshed user experience.

Stratux NX is developed and maintained by Lemonpot. It is not a replacement for upstream Stratux, nor is it represented as an official upstream release.

## Key Features

### Automatic Flight Log

Automatically captures flight activity and creates a useful record of each flight, reducing manual log entry and making recent flight information easier to review.

### Internet Data Management

Provides tools for managing internet-connected data alongside the Stratux experience, with controls designed for practical use in the aircraft.

### Modern UI

A redesigned, responsive interface makes important status information and controls easier to understand and use across phones, tablets, and desktop browsers.

### Offline Airport and Timezone Database

Includes local airport and timezone data so core location-aware functionality remains available without an internet connection.

### Adaptive AHRS v2 Beta

Introduces an adaptive AHRS implementation designed to improve attitude estimation across changing flight conditions.

> **Beta notice:** Adaptive AHRS v2 is under active development and should be treated as experimental. Always validate its behavior against certified aircraft instruments and maintain an appropriate visual and instrument cross-check.

## Relationship to Stratux

Stratux NX would not exist without the work of the Stratux project and its contributors.

- **Upstream project:** [cyoung/stratux](https://github.com/cyoung/stratux)
- **Stratux NX:** an independently developed and maintained downstream distribution
- **Maintainer:** [Lemonpot](https://lemonpot.ai/)
- **Compatibility and changes:** Stratux NX may add, modify, or replace components relative to upstream Stratux

We aim to preserve clear attribution and a constructive relationship with the upstream community. Features specific to Stratux NX should be reported in this repository; issues reproducible in unmodified upstream Stratux may also belong in the upstream project.

## Project Status

Stratux NX is under active development. Interfaces, behavior, and experimental features may change between releases. Release notes should be reviewed before updating equipment used in flight.

## Safety

Stratux NX is intended to provide supplemental situational awareness only. It is not certified avionics and must not be used as the sole source of navigation, traffic, weather, attitude, terrain, or flight information.

The pilot in command remains responsible for using approved data, maintaining appropriate visual and instrument references, and operating in accordance with all applicable regulations and aircraft limitations.

## Contributing

Bug reports, feature requests, and contributions are welcome through this repository. When reporting an issue, please include the Stratux NX version, hardware configuration, connected devices, and enough detail to reproduce the behavior.

For upstream Stratux development, documentation, and community resources, visit the [official Stratux repository](https://github.com/cyoung/stratux).

## Acknowledgements

Stratux NX includes and builds upon work created by the Stratux community. We gratefully acknowledge Christopher Young and all upstream contributors whose open-source work made this project possible.

Stratux and related names belong to their respective owners. Lemonpot's changes and additions are maintained independently in this repository.
