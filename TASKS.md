# Gearbox - Scratch Pad

Describe features, bugs, or ideas below. Claude will break them into GitHub issues and add them to the project board.

Reminder to create a branch and item on the board along with PR as per your instructions in CLAUDE.md

---

## Dashboard Editor

The dashboard system needs a lot of work.  Here's what I have so far.
Create a single new branch called 'dashboard-editor-improvements'

- When I open the dashboard editor we get several warning and errors in the browser console
  ```
  cdn.tailwindcss.com should not be used in production. To use Tailwind CSS in production, install it as a PostCSS plugin or use the Tailwind CLI: https://tailwindcss.com/docs/installation tailwind.js:64:1711
  Loading failed for the <script> with source “https://cdn.jsdelivr.net/npm/sortablejs@1.15.0/Sortable.min.js”. dashboards:774:8804
  Content-Security-Policy: The page’s settings blocked a script (script-src-elem) at https://cdn.jsdelivr.net/npm/sortablejs@1.15.0/Sortable.min.js from being executed because it violates the following directive: “script-src 'self' 'unsafe-inline'” dashboards
```
- When I click on a predefined plugin dashboard we get the follow browser console errors:
  ```
  cdn.tailwindcss.com should not be used in production. To use Tailwind CSS in production, install it as a PostCSS plugin or use the Tailwind CLI: https://tailwindcss.com/docs/installation tailwind.js:64:1711
  XHR
  GET
  http://localhost:3000/api/light-hugger/services/overview
  [HTTP/1.1 404 Not Found 4ms]

  XHR
  GET
  http://localhost:3000/api/light-hugger/services/failed?hide_when_empty=false
  [HTTP/1.1 404 Not Found 1ms]

  Response Status Error Code 404 from /api/light-hugger/services/overview htmx.min.js:1:26530
  Response Status Error Code 404 from /api/light-hugger/services/failed?hide_when_empty=false htmx.min.js:1:26530
  XHR
  GET
  http://localhost:3000/api/light-hugger/services/overview
  [HTTP/1.1 404 Not Found 5ms]

  Response Status Error Code 404 from /api/light-hugger/services/overview htmx.min.js:1:26530
  XHR
  GET
  http://localhost:3000/api/light-hugger/services/failed?hide_when_empty=false
  [HTTP/1.1 404 Not Found 2ms]

  Response Status Error Code 404 from /api/light-hugger/services/failed?hide_when_empty=false htmx.min.js:1:26530
  ```
  The dashboard editor shows json and not the widgets themselves
  ```
  System Services
  {"services":[{"name":"fail2ban","status":"active","active":true,"description":"Fail2Ban Service","started_at":"2026-01-28T06:43:51Z","uptime":"4d 20h"},{"name":"gearbox-agent","status":"active","active":true,"description":"Gearbox Agent - Server monitoring and management agent","started_at":"2026-02-01T21:55:02Z","uptime":"4h 52m"},{"name":"haproxy","status":"active","active":true,"description":"HAProxy Load Balancer","started_at":"2026-01-28T06:43:52Z","uptime":"4d 20h"},{"name":"nftables","status":"active","active":true,"description":"nftables","started_at":"2026-01-04T23:28:39Z","uptime":"28d 3h"}]} 
  ```
- When I create a new dashboard then edit it the available widgets box has a spinner that never stops
  - The widget selection system is supposed to be a drawer that slides out from the left over the main menu.  I should be able to filter and apply other controls to find the widgets I want from the enabled plugins.
  - The new dashboard dialog should allow me to select an icon for the menu
  - The dashboard editor should allow me to change the title of the dashboard
