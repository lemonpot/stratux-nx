angular.module('appControllers').controller('UpdateCtrl', ['$scope', '$http', '$interval',
  function($scope, $http, $interval) {
    $scope.status = {};
    $scope.checking = false;
    $scope.installing = false;

    function loadStatus() {
      $http.get('/update/status').then(function(resp) {
        $scope.status = resp.data || {};
      });
    }

    $scope.checkNow = function() {
      $scope.checking = true;
      $http.get('/update/check').then(function(resp) {
        $scope.status = resp.data || {};
        $scope.checking = false;
      }, function() {
        $scope.checking = false;
        if (window.StratuxUI) {
          window.StratuxUI.toast('Could not reach update server', 'error');
        }
      });
    };

    $scope.installUpdate = function() {
      $scope.installing = true;
      $http.post('/update/install').then(function(resp) {
        $scope.installing = false;
        if (window.StratuxUI) {
          window.StratuxUI.toast('Update download started', 'success');
        }
      }, function() {
        $scope.installing = false;
        if (window.StratuxUI) {
          window.StratuxUI.toast('Failed to start update', 'error');
        }
      });
    };

    $scope.formatTime = function(t) {
      if (!t || t === '0001-01-01T00:00:00Z') return 'Never';
      try {
        var d = new Date(t);
        if (isNaN(d.getTime())) return 'Unknown';
        var now = new Date();
        var diff = Math.floor((now - d) / 1000);
        if (diff < 60) return 'Just now';
        if (diff < 3600) return Math.floor(diff / 60) + 'm ago';
        if (diff < 86400) return Math.floor(diff / 3600) + 'h ago';
        return d.toLocaleDateString();
      } catch (e) {
        return 'Unknown';
      }
    };

    $scope.formatSize = function(bytes) {
      if (!bytes || bytes <= 0) return 'Unknown size';
      if (bytes < 1024) return bytes + ' B';
      if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB';
      return (bytes / 1048576).toFixed(1) + ' MB';
    };

    // Initial load.
    loadStatus();

    // Poll while downloading.
    var poller = $interval(function() {
      if ($scope.status.downloading) {
        loadStatus();
      }
    }, 2000);

    $scope.$on('$destroy', function() {
      $interval.cancel(poller);
    });
  }
]);
