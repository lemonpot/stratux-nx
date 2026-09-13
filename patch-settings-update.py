#!/usr/bin/env python3
"""Embed the Software Update panel into the Settings page."""
import pathlib
import sys

root = pathlib.Path(sys.argv[1])

UPDATE_PANEL = '''\
        <!-- Software Update (embedded from UpdateCtrl) -->
        <div class="panel-group col-sm-12">
            <div class="panel panel-default sx-update-panel" ng-controller="UpdateCtrl">
                <div class="panel-heading">
                    <span>Software Update</span>
                    <span class="sx-update-status-badge" ng-if="status.available.update_available" style="float:right;">
                        <i class="fa fa-arrow-circle-up"></i> Update available
                    </span>
                </div>
                <div class="panel-body">
                    <div class="sx-update-version-row">
                        <div><strong>Current version</strong><br><span class="sx-mono">{{status.current_version || \'Unknown\'}}</span></div>
                        <div><strong>Last checked</strong><br><span>{{formatTime(status.last_check)}}</span></div>
                        <button class="btn btn-default btn-sm" ng-click="checkNow()" ng-disabled="checking">
                            <i class="fa" ng-class="checking ? \'fa-circle-o-notch fa-spin\' : \'fa-refresh\'"></i>
                            {{checking ? \'Checking...\' : \'Check now\'}}
                        </button>
                    </div>

                    <div class="sx-update-available" ng-if="status.available.update_available">
                        <div class="sx-update-info">
                            <i class="fa fa-gift"></i>
                            <div>
                                <strong>Version {{status.available.version}}</strong>
                                <span>{{formatSize(status.available.size)}} &middot; Published {{status.available.published_at || \'recently\'}}</span>
                            </div>
                        </div>

                        <div ng-if="!status.downloading && !status.staged && !status.download_error">
                            <button class="btn btn-primary btn-block sx-update-btn" ng-click="installUpdate()" ng-disabled="installing">
                                <i class="fa" ng-class="installing ? \'fa-circle-o-notch fa-spin\' : \'fa-download\'"></i>
                                {{installing ? \'Starting...\' : \'Download and install\'}}
                            </button>
                        </div>

                        <div ng-if="status.downloading">
                            <div class="sx-update-progress-label">Downloading... {{status.download_percent}}%</div>
                            <div class="progress"><div class="progress-bar progress-bar-primary" ng-style="{\'width\': status.download_percent + \'%\'}"></div></div>
                            <div class="sx-update-hint">Do not disconnect power or internet.</div>
                        </div>

                        <div class="alert alert-success" ng-if="status.staged">
                            <i class="fa fa-check-circle"></i> Update verified. Rebooting to install...
                        </div>

                        <div class="alert alert-danger" ng-if="status.download_error">
                            <i class="fa fa-exclamation-triangle"></i> {{status.download_error}}
                            <button class="btn btn-sm btn-danger" ng-click="installUpdate()" style="margin-left:10px;">Retry</button>
                        </div>
                    </div>

                    <div class="sx-update-uptodate" ng-if="!status.available && !status.check_error && status.last_check && status.last_check !== \'0001-01-01T00:00:00Z\'">
                        <i class="fa fa-check-circle text-success"></i> Your Stratux NX is up to date.
                    </div>

                    <div class="alert alert-warning" ng-if="status.check_error">
                        <i class="fa fa-exclamation-triangle"></i> {{status.check_error}}
                    </div>
                </div>
            </div>
        </div>
'''

p = root / "web/plates/settings.html"
s = p.read_text()

if 'sx-update-panel' in s:
    print("Settings update panel already present -- skipping.")
    sys.exit(0)

marker = '        <!-- AHRS Options -->'
if marker not in s:
    raise SystemExit('Could not find <!-- AHRS Options --> marker in web/plates/settings.html')

s = s.replace(marker, UPDATE_PANEL + marker, 1)

p.write_text(s)
print("Software Update panel embedded into Settings page.")
