<!--
SPDX-License-Identifier: MPL-2.0
This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->

# Licensing

Carbon Panel is a fork of DiscoPanel (https://github.com/discohaus/discopanel).

The fork point in this repository is commit 7763fa1082793720031a3bc7e64e0fd9830216d7
(tree a160c91ca2379be5ca20247ad4c30e39d3d077a1). That tree is DiscoPanel commit
ba7eb27c9728bb34999f16d77838dfd17a53d191. History before the fork stays under
the MIT License and is not relicensed.

## MIT only

LICENSE is the MIT License, Copyright (c) 2025 Nicholas W. Heyer. That
copyright notice and permission notice remain on material inherited from
DiscoPanel.

A file is MIT only when its bytes are unchanged from the fork tree. That
includes a byte-for-byte copy at another path. Those files do not carry an
MPL notice.

These paths are not MPL-2.0:

- LICENSE
- web/carbon-panel/src/lib/proto/gnostic/openapi/v3/annotations_pb.ts
  (Copyright 2022 Google LLC)
- web/carbon-panel/src/lib/proto/gnostic/openapi/v3/openapiv3_pb.ts
  (Copyright 2020 Google LLC)
- go.mod, go.sum, and lockfiles (package-lock.json, bun.lock, buf.lock,
  flake.lock, and other *.lock files). They are dependency metadata.
  Third-party modules keep their own licenses.
- vendor/, third_party/, and node_modules/

## MPL-2.0

LICENSE.MPL-2.0 is the Mozilla Public License, v. 2.0.

MPL-2.0 covers original files added after the fork by the carbon-panel
maintainers. It also covers the maintainers' changes to inherited files.

- A maintainer-owned file that was not in the fork tree carries
  SPDX-License-Identifier: MPL-2.0 and the MPL Exhibit A notice.
- An inherited file that the maintainers changed carries
  SPDX-License-Identifier: MIT AND MPL-2.0. The MIT license still applies
  to the DiscoPanel material. The new expression is MPL-2.0.
- JSON files and other files that cannot hold a header follow the same
  split. Lockfiles and the paths listed above are the exception: they are
  not MPL text.
- Non-text files added or changed after the fork follow the same split.
  They have no in-file header.

The SPDX identifier in a file is the file-level boundary. A file with no
MPL identifier whose bytes match the fork tree is MIT only.
