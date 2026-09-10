#!/usr/bin/env python3
import pathlib
import sys

root = pathlib.Path(sys.argv[1])

p = root / 'web/plates/js/flightlog.js'
s = p.read_text()
marker = '''    $scope.dualTime = function(value) {\n        if (!value) return '--';\n        return $scope.formatZulu(value) + ' · ' + $scope.formatLocal(value);\n    };'''
helper = marker + '''\n\n    $scope.formatLocalInZone = function(value, zone) {\n        var d = validDate(value);\n        if (!d) return '--';\n        zone = zone || timezone();\n        try {\n            return new Intl.DateTimeFormat('en-CA', {\n                timeZone: zone, year:'numeric', month:'short', day:'2-digit',\n                hour:'2-digit', minute:'2-digit', second:'2-digit', hour12:false\n            }).format(d);\n        } catch (e) {\n            return localParts(value, true);\n        }\n    };\n\n    $scope.dualTimeAtAirport = function(value, airport) {\n        if (!value) return '--';\n        var zone = airport && airport.Timezone ? airport.Timezone : timezone();\n        return $scope.formatZulu(value) + ' · ' + $scope.formatLocalInZone(value, zone) + ' ' + zone;\n    };\n\n    $scope.localDateAtAirport = function(value, airport) {\n        var d = validDate(value);\n        if (!d) return '--';\n        var zone = airport && airport.Timezone ? airport.Timezone : timezone();\n        try {\n            return new Intl.DateTimeFormat('en-CA', {timeZone:zone, year:'numeric', month:'short', day:'2-digit'}).format(d);\n        } catch (e) {\n            return $scope.formatLocalDate(value);\n        }\n    };'''
if '$scope.dualTimeAtAirport' not in s:
    if marker not in s:
        raise SystemExit('Could not add airport-aware local event time helpers')
    s = s.replace(marker, helper, 1)
p.write_text(s)

p = root / 'web/plates/flightlog.html'
s = p.read_text()
s = s.replace('{{dualTime(data.Current.OffBlockUTC)}}', '{{dualTimeAtAirport(data.Current.OffBlockUTC, data.Current.DepartureAirport)}}')
s = s.replace('{{dualTime(data.Current.TakeoffUTC)}}', '{{dualTimeAtAirport(data.Current.TakeoffUTC, data.Current.DepartureAirport)}}')
s = s.replace('{{dualTime(data.Current.LandingUTC)}}', '{{dualTimeAtAirport(data.Current.LandingUTC, data.Current.ArrivalAirport)}}')
s = s.replace('{{dualTime(data.Current.OnBlockUTC)}}', '{{dualTimeAtAirport(data.Current.OnBlockUTC, data.Current.ArrivalAirport)}}')
s = s.replace('{{formatLocalDate(flight.TakeoffUTC || flight.OffBlockUTC)}}', '{{localDateAtAirport(flight.TakeoffUTC || flight.OffBlockUTC, flight.DepartureAirport)}}')
s = s.replace('{{formatLocalDate(selectedFlight.TakeoffUTC || selectedFlight.OffBlockUTC)}}', '{{localDateAtAirport(selectedFlight.TakeoffUTC || selectedFlight.OffBlockUTC, selectedFlight.DepartureAirport)}}')
s = s.replace('{{formatLocal(selectedFlight.TakeoffUTC)}}', '{{formatLocalInZone(selectedFlight.TakeoffUTC, selectedFlight.DepartureAirport.Timezone)}}')
s = s.replace('{{formatLocal(selectedFlight.LandingUTC)}}', '{{formatLocalInZone(selectedFlight.LandingUTC, selectedFlight.ArrivalAirport.Timezone)}}')
p.write_text(s)
