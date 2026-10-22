#!/usr/bin/env python3
"""Patch status.js to fetch and display flight storage info from /flightLog."""
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
p = root / "web" / "plates" / "js" / "status.js"
s = p.read_text()

# Add a periodic fetch of /flightLog to populate FlightStorageFree/Total
# Insert right after the DiskSpace assignment block
old = """var MiBFree = status.DiskBytesFree/1048576;
			$scope.DiskSpace = MiBFree.toFixed(1);"""

new = """var MiBFree = status.DiskBytesFree/1048576;
			$scope.DiskSpace = MiBFree.toFixed(1);

			if (!$scope._flightStoragePending) {
				$scope._flightStoragePending = true;
				$http.get(URL_FLIGHTLOG_GET || '/flightLog').then(function(r) {
					var d = r.data || {};
					if (d.StorageFreeBytes !== undefined) {
						$scope.FlightStorageFree = (d.StorageFreeBytes / 1048576).toFixed(1);
						$scope.FlightStorageTotal = (d.StorageTotalBytes / 1048576).toFixed(0);
					}
					$scope._flightStoragePending = false;
				}, function() { $scope._flightStoragePending = false; });
			}"""

if old in s:
    s = s.replace(old, new, 1)
    p.write_text(s)
    print("Patched status.js with flight storage display")
elif "FlightStorageFree" in s:
    print("status.js already has flight storage patch")
else:
    raise SystemExit("Could not patch status.js: DiskSpace block not found")
