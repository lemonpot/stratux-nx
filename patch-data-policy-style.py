#!/usr/bin/env python3
import pathlib, sys
root = pathlib.Path(sys.argv[1])
p = root / 'web/css/datausage.css'
s = p.read_text()
marker = '/* unrestricted-device-controls */'
css = '''
/* unrestricted-device-controls */
.data-safety-actions{display:flex;align-items:center;gap:12px;flex-wrap:wrap;justify-content:flex-end}.data-session-reset{border:1px solid #d1d5db;background:#fff;color:#374151;border-radius:10px;padding:8px 11px;font-size:11px;font-weight:800}.data-session-reset:hover{border-color:#93c5fd;background:#eff6ff;color:#1d4ed8}.data-session-reset.is-urgent{border-color:#f59e0b;background:#fffbeb;color:#b45309;box-shadow:0 0 0 3px rgba(245,158,11,.10)}.data-device-actions{display:flex;align-items:center;gap:7px;flex:0 0 auto}.data-policy-btn{border:1px solid #d1d5db;background:#fff;color:#475569;border-radius:10px;font-size:10px;font-weight:800;min-width:112px;padding:8px 9px}.data-policy-btn:hover{border-color:#93c5fd;background:#eff6ff;color:#1d4ed8}.data-policy-btn.is-exempt{border-color:#a7f3d0;background:#ecfdf5;color:#047857}.data-status-badge.unrestricted{background:#ecfdf5;color:#047857;padding:2px 7px}.data-device-progress-label .unrestricted{color:#047857;font-weight:650}@media(max-width:760px){.data-safety-actions{width:100%;justify-content:space-between}.data-session-reset{order:2;flex:1}.data-device-actions{width:100%;display:grid;grid-template-columns:1fr 1fr}.data-policy-btn,.data-block-btn{width:100%;min-width:0}}
'''
if marker not in s:
    p.write_text(s + css)
