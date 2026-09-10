appControllers.directive('flowMonitor', function($http, $interval) {
    return {
        restrict: 'A',
        controller: function($scope) {
            $scope.flowData = {
                Flows: [],
                Services: [],
                TrackedBytes: 0,
                ActiveConnections: 0,
                IdentifiedConnections: 0,
                PacketInspection: false,
                ConntrackAccounting: false,
                VisibilityNote: ''
            };
            $scope.flowSearch = '';
            $scope.flowError = '';
            $scope.serviceGroups = [];
            $scope.expandedGroups = {};
            $scope.showAllGroups = false;

            var rankOrder = [];
            var lastRankAt = 0;
            var rankIntervalMs = 10000;

            $scope.formatBytes = function(bytes) {
                bytes = Number(bytes || 0);
                if (bytes < 1024) return bytes.toFixed(0) + ' B';
                if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
                if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
                return (bytes / (1024 * 1024 * 1024)).toFixed(2) + ' GB';
            };

            $scope.formatRate = function(bytesPerSecond) {
                bytesPerSecond = Number(bytesPerSecond || 0);
                if (bytesPerSecond < 1024) return bytesPerSecond.toFixed(0) + ' B/s';
                if (bytesPerSecond < 1024 * 1024) return (bytesPerSecond / 1024).toFixed(1) + ' KB/s';
                return (bytesPerSecond / (1024 * 1024)).toFixed(2) + ' MB/s';
            };

            $scope.connectionLabel = function(flow) {
                return flow.ClientIP + ':' + flow.ClientPort + ' → ' + flow.RemoteIP + ':' + flow.RemotePort;
            };

            $scope.groupIconClass = function(group) {
                var c = String(group.Category || '').toLowerCase();
                var n = String(group.Service || '').toLowerCase();
                if (c.indexOf('streaming video') >= 0 || n.indexOf('youtube') >= 0 || n.indexOf('netflix') >= 0 || n.indexOf('tiktok') >= 0) return 'fa-play-circle';
                if (c.indexOf('streaming audio') >= 0 || n.indexOf('spotify') >= 0 || n.indexOf('tidal') >= 0) return 'fa-music';
                if (c.indexOf('software update') >= 0) return 'fa-download';
                if (c.indexOf('social') >= 0) return 'fa-comments';
                if (c.indexOf('cloud') >= 0) return 'fa-cloud';
                if (c.indexOf('ai') >= 0 || n.indexOf('chatgpt') >= 0 || n.indexOf('openai') >= 0) return 'fa-bolt';
                return 'fa-globe';
            };

            function fallbackGroupsFromFlows(flows) {
                var groups = {};
                angular.forEach(flows || [], function(flow) {
                    var name = flow.Service || flow.Hostname || (flow.RemoteIP + ':' + flow.RemotePort) || 'Unknown';
                    var key = 'fallback|' + name;
                    if (!groups[key]) {
                        groups[key] = {
                            Key: key,
                            Service: name,
                            Category: flow.Category || 'Internet service',
                            UploadBytes: 0,
                            DownloadBytes: 0,
                            TotalBytes: 0,
                            CurrentBps: 0,
                            ActiveConnections: 0,
                            Hosts: []
                        };
                    }
                    var g = groups[key];
                    g.UploadBytes += Number(flow.UploadBytes || 0);
                    g.DownloadBytes += Number(flow.DownloadBytes || 0);
                    g.TotalBytes += Number(flow.TotalBytes || 0);
                    g.CurrentBps += Number(flow.CurrentBps || 0);
                    g.ActiveConnections++;
                    var host = flow.Hostname || flow.RemoteIP;
                    if (host && g.Hosts.indexOf(host) < 0 && g.Hosts.length < 4) g.Hosts.push(host);
                });
                var out = [];
                angular.forEach(groups, function(group) { out.push(group); });
                return out;
            }

            function flowBelongsToGroup(flow, group) {
                if (!flow || !group) return false;
                if (flow.Service === group.Service) {
                    if (group.Category === 'Unknown' && group.Hosts && group.Hosts.length) {
                        var endpoint = flow.Hostname || flow.RemoteIP;
                        return group.Hosts.indexOf(endpoint) >= 0;
                    }
                    return true;
                }
                if (flow.Hostname && flow.Hostname === group.Service) return true;
                return false;
            }

            $scope.rebuildServiceGroups = function(forceSort) {
                var flows = $scope.flowData.Flows || [];
                var source = ($scope.flowData.Services && $scope.flowData.Services.length) ?
                    $scope.flowData.Services : fallbackGroupsFromFlows(flows);
                var groups = [];
                var totalTracked = Number($scope.flowData.TrackedBytes || 0);

                angular.forEach(source, function(src) {
                    var group = {
                        Key: src.Key || ('service|' + (src.Service || src.Name || 'Unknown')),
                        Service: src.Service || src.Name || 'Unknown service',
                        Category: src.Category || 'Internet service',
                        UploadBytes: Number(src.UploadBytes || 0),
                        DownloadBytes: Number(src.DownloadBytes || 0),
                        TotalBytes: Number(src.TotalBytes || 0),
                        CurrentBps: Number(src.CurrentBps || 0),
                        ActiveConnections: Number(src.ActiveConnections || src.Connections || 0),
                        Hosts: (src.Hosts || []).slice(0, 4),
                        Flows: []
                    };
                    angular.forEach(flows, function(flow) {
                        if (flowBelongsToGroup(flow, group)) group.Flows.push(flow);
                    });
                    group.Flows.sort(function(a, b) {
                        return Number(b.TotalBytes || 0) - Number(a.TotalBytes || 0);
                    });
                    groups.push(group);
                });

                if (!totalTracked) {
                    angular.forEach(groups, function(group) { totalTracked += group.TotalBytes; });
                }

                var now = Date.now();
                if (forceSort || !rankOrder.length || (now - lastRankAt) >= rankIntervalMs) {
                    groups.sort(function(a, b) {
                        if (b.TotalBytes !== a.TotalBytes) return b.TotalBytes - a.TotalBytes;
                        return b.CurrentBps - a.CurrentBps;
                    });
                    rankOrder = [];
                    angular.forEach(groups, function(group) { rankOrder.push(group.Key); });
                    lastRankAt = now;
                } else {
                    var positions = {};
                    angular.forEach(rankOrder, function(key, index) { positions[key] = index; });
                    groups.sort(function(a, b) {
                        var ap = Object.prototype.hasOwnProperty.call(positions, a.Key) ? positions[a.Key] : 999999;
                        var bp = Object.prototype.hasOwnProperty.call(positions, b.Key) ? positions[b.Key] : 999999;
                        if (ap !== bp) return ap - bp;
                        return b.TotalBytes - a.TotalBytes;
                    });
                }

                var maxBytes = 0;
                var totalLiveBps = 0;
                angular.forEach(groups, function(group) {
                    if (group.TotalBytes > maxBytes) maxBytes = group.TotalBytes;
                    totalLiveBps += group.CurrentBps;
                });
                angular.forEach(groups, function(group, index) {
                    group.Rank = index + 1;
                    group.UsagePercent = maxBytes > 0 ? Math.max(1, (group.TotalBytes / maxBytes) * 100) : 0;
                    group.SharePercent = totalTracked > 0 ? (group.TotalBytes / totalTracked) * 100 : 0;
                    group.HostSummary = group.Hosts.length ? group.Hosts.join(' · ') : 'Hostname not resolved yet';
                });

                $scope.serviceGroups = groups;
                $scope.flowData.TrackedBytes = totalTracked;
                $scope.totalLiveBps = totalLiveBps;
                $scope.topConsumer = groups.length ? groups[0] : null;
            };

            $scope.groupMatchesSearch = function(group) {
                var q = String($scope.flowSearch || '').toLowerCase().trim();
                if (!q) return true;
                var text = [group.Service, group.Category, group.HostSummary, group.Key].join(' ').toLowerCase();
                return text.indexOf(q) >= 0;
            };

            $scope.toggleGroup = function(group) {
                if (!group) return;
                $scope.expandedGroups[group.Key] = !$scope.expandedGroups[group.Key];
            };

            $scope.displayLimit = function() {
                if ($scope.flowSearch) return 500;
                return $scope.showAllGroups ? 500 : 12;
            };

            $scope.refreshFlows = function(forceRanking) {
                $http.get('/dataUsage/flows', {cache: false}).then(function(response) {
                    var d = response.data || {};
                    d.Flows = d.Flows || [];
                    d.Services = d.Services || [];
                    $scope.flowData = d;
                    $scope.flowError = '';
                    $scope.rebuildServiceGroups(!!forceRanking);
                }, function() {
                    $scope.flowError = 'Unable to read live Internet connections.';
                });
            };

            $scope.killFlow = function(flow) {
                if (!flow || flow.actionPending || flow.RecentlyKilled) return;
                flow.actionPending = true;
                $http.post('/dataUsage/flow/kill', {id: flow.ID}).then(function() {
                    flow.actionPending = false;
                    flow.RecentlyKilled = true;
                    window.setTimeout(function() { $scope.refreshFlows(false); }, 350);
                }, function(response) {
                    flow.actionPending = false;
                    $scope.flowError = 'Could not kill this connection: ' + ((response && response.data) || 'unknown error');
                });
            };

            $scope.refreshFlows(true);
            var timer = $interval(function() { $scope.refreshFlows(false); }, 2000);
            $scope.$on('$destroy', function() {
                $interval.cancel(timer);
            });
        }
    };
});
