# Stratux NX — Open-source and third-party notices

Stratux NX is an independent distribution maintained by Lemonpot. It combines
Lemonpot-authored customizations with the Stratux project and other
open-source components. It is not an official upstream Stratux release, and
the names of upstream authors or contributors are not used to imply
endorsement.

## Lemonpot customizations

Copyright © 2026 Lemonpot.

The original files and modifications published in the
[Stratux NX repository](https://github.com/lemonpot/stratux-nx) are licensed
under the BSD 3-Clause License in `LICENSE`, except where a file states
otherwise.

## Stratux

Stratux NX is built from the current community-maintained
[stratux/stratux](https://github.com/stratux/stratux) project, which continues
the original [cyoung/stratux](https://github.com/cyoung/stratux) work founded
by Christopher Young and other contributors.

Stratux is licensed under the BSD 3-Clause License. Its copyright notice,
license conditions and disclaimer are preserved in the corresponding source
archive and in the installed Debian package.

The original project notice includes:

> Copyright (c) 2015-2016 Christopher Young ("Copyright Holder").
> All rights reserved.

## GPL components

The distribution includes components under the GNU General Public License:

- `dump978` — GPL version 2.
- FlightAware `dump1090` — GPL version 2 or any later version, with portions
  also carrying BSD-style notices.
- `rtl-ais` — GPL version 2 or any later version, incorporating notices from
  its upstream contributors.

These licenses apply to their respective components. They do not replace the
license stated for independently authored Stratux NX files.

## Other dependencies

Stratux and Stratux NX also use open-source Go modules, JavaScript libraries,
map components and operating-system packages under their respective licenses.
The build collects available license and notice files for bundled components
into `/usr/share/doc/stratux/licenses/` in the installed package.

## Corresponding source

Each published Stratux NX OTA release includes a permanent corresponding-source
archive alongside the `.deb` on the
[Stratux NX releases page](https://github.com/lemonpot/stratux-nx/releases).
That archive records the exact upstream Stratux source, initialized submodules
and Lemonpot customization source used for the binary.

For source or licensing questions, contact
[tomas@lemonpot.com](mailto:tomas@lemonpot.com).

## No warranty and aviation use

The software is provided without warranty under the terms of its applicable
licenses. Stratux NX provides supplemental situational awareness only. It is
not certified avionics and must not be used as the sole source for navigation,
traffic avoidance, weather decisions or flight safety.
