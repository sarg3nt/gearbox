# Gearbox - Scratch Pad

Describe features, bugs, or ideas below. Claude will break them into GitHub issues and add them to the project board.


Create a branch for fixing various bugs.

- Clicking the edit icon in the nav bar throws an error in the console and the menu items are not movable.  Error: Sortable not loaded!
- There's a lot of debug info being spit out into the ssh console
  ```
  time=2026-02-01T15:31:18.022-08:00 level=INFO msg="🔍 GetUser START" path=/api/session-info
  time=2026-02-01T15:31:18.022-08:00 level=INFO msg="👤 User ID from session" user_id=591e5fcb-92ed-44ba-bfc9-7662321ee131
  time=2026-02-01T15:31:18.022-08:00 level=INFO msg="🔑 Session token from cookie" token_length=32
  time=2026-02-01T15:31:18.022-08:00 level=INFO msg="⏰ Session time valid" age=25m2.02221s
  time=2026-02-01T15:31:18.022-08:00 level=INFO msg="👤 User found in DB" email=dave@sarg3.net
  2026/02/01 15:31:18 📝 Request to /api/alerts/count - injecting useLocalAssets=true into context
  time=2026-02-01T15:31:18.022-08:00 level=INFO msg="🔍 GetUser START" path=/api/alerts/count
  time=2026-02-01T15:31:18.022-08:00 level=INFO msg="👤 User ID from session" user_id=591e5fcb-92ed-44ba-bfc9-7662321ee131
  time=2026-02-01T15:31:18.022-08:00 level=INFO msg="🔑 Session token from cookie" token_length=32
  time=2026-02-01T15:31:18.022-08:00 level=INFO msg="⏰ Session time valid" age=25m2.022631s
  time=2026-02-01T15:31:18.022-08:00 level=INFO msg="✅ Session token validated against DB"
  time=2026-02-01T15:31:18.022-08:00 level=INFO msg="✅ GetUser COMPLETE" user_id=591e5fcb-92ed-44ba-bfc9-7662321ee131
  time=2026-02-01T15:31:18.022-08:00 level=INFO msg="🔍 GetUser START" path=/api/session-info
  time=2026-02-01T15:31:18.022-08:00 level=INFO msg="👤 User ID from session" user_id=591e5fcb-92ed-44ba-bfc9-7662321ee131
  time=2026-02-01T15:31:18.022-08:00 level=INFO msg="🔑 Session token from cookie" token_length=32
  time=2026-02-01T15:31:18.022-08:00 level=INFO msg="⏰ Session time valid" age=25m2.022694s
  time=2026-02-01T15:31:18.022-08:00 level=INFO msg="👤 User found in DB" email=dave@sarg3.net
  time=2026-02-01T15:31:18.022-08:00 level=INFO msg="👤 User found in DB" email=dave@sarg3.net
  time=2026-02-01T15:31:18.022-08:00 level=INFO msg="✅ Session token validated against DB"
  time=2026-02-01T15:31:18.022-08:00 level=INFO msg="✅ GetUser COMPLETE" user_id=591e5fcb-92ed-44ba-bfc9-7662321ee131
  time=2026-02-01T15:31:18.022-08:00 level=INFO msg="🔍 GetUser START" path=/api/alerts/count
  time=2026-02-01T15:31:18.022-08:00 level=INFO msg="👤 User ID from session" user_id=591e5fcb-92ed-44ba-bfc9-7662321ee131
  time=2026-02-01T15:31:18.022-08:00 level=INFO msg="🔑 Session token from cookie" token_length=32
  time=2026-02-01T15:31:18.022-08:00 level=INFO msg="⏰ Session time valid" age=25m2.022955s
  time=2026-02-01T15:31:18.023-08:00 level=INFO msg="✅ Session token validated against DB"
  time=2026-02-01T15:31:18.023-08:00 level=INFO msg="✅ GetUser COMPLETE" user_id=591e5fcb-92ed-44ba-bfc9-7662321ee131
  time=2026-02-01T15:31:18.023-08:00 level=INFO msg="👤 User found in DB" email=dave@sarg3.net
  ```
- Logs plugin throws this error in browser console.
  GET
  http://localhost:3000/api/light-hugger/log-sources
  [HTTP/1.1 500 Internal Server Error 1ms]
  Failed to load log sources: SyntaxError: JSON.parse: unexpected character at line 1 column 1 of the JSON data logs:876:13
- Services plugin throws this in the browser console.
  GET
  http://localhost:3000/api/light-hugger/log-sources
  [HTTP/1.1 500 Internal Server Error 4ms]

  SSE connected for services services:824:13
  Failed to load log sources: SyntaxError: JSON.parse: unexpected character at line 1 column 1 of the JSON data services:881:13

- Traffic plugin throws this error in the browser console
  Failed to refresh traffic data: TypeError: can't access property "selectAll", g is undefined
    updateVisualizationData http://localhost:3000/static/js/traffic/traffic-visualization.js:1690
    updateNetworkVisualization http://localhost:3000/static/js/traffic/traffic-visualization.js:1254
    updateUI http://localhost:3000/static/js/traffic/traffic-visualization.js:399
    refreshTrafficData http://localhost:3000/static/js/traffic/traffic-visualization.js:331
    initSSE http://localhost:3000/static/js/traffic/traffic-visualization.js:2285
    initSSE http://localhost:3000/static/js/traffic/traffic-visualization.js:2284
    <anonymous> http://localhost:3000/static/js/traffic/traffic-visualization.js:2366
    EventListener.handleEvent* http://localhost:3000/static/js/traffic/traffic-visualization.js:2363
    traffic-visualization.js:333:11
- Going to the root of the app `http://localhost:3000/` goes to a blank page instead of the first plugin in the nav list
- Refreshing to Alerts page too quickly is causing the backend to seemingly lock up and it repeats these logs
  2026/02/01 15:43:07 📝 Request to /alerts - injecting useLocalAssets=true into context
  time=2026-02-01T15:43:07.664-08:00 level=INFO msg="🔍 GetUser START" path=/alerts
  time=2026-02-01T15:43:07.664-08:00 level=INFO msg="👤 User ID from session" user_id=591e5fcb-92ed-44ba-bfc9-7662321ee131
  time=2026-02-01T15:43:07.664-08:00 level=INFO msg="🔑 Session token from cookie" token_length=32
  time=2026-02-01T15:43:07.664-08:00 level=INFO msg="⏰ Session time valid" age=36m51.664241s

  Same with services. Quitting the server with ctrl+c and rerun with `make dev` fixes it until the next time. This happens every time.  Page says "Error loading services: NetworkError when attempting to fetch resource."  Last line in logs is "time=2026-02-01T15:38:18.220-08:00 level=INFO msg="⏰ Session time valid" age=32m2.220582s". gearbox-agent logs look fine.
  No other links in the app work until we kill and restart gearbox
- OS Updates throws the following in the browser console.  It should be loading local resources in dev.
  The connection to http://localhost:3000/api/events?server=light-hugger was interrupted while the page was loading. os-updates:839:20
  cdn.tailwindcss.com should not be used in production. To use Tailwind CSS in production, install it as a PostCSS plugin or use the Tailwind CLI: https://tailwindcss.com/docs/installation tailwind.js:64:1711
- /settings/admin/disabled-entities throws this error in the browser console:  Note: there are currently no disabled entities.  
  Uncaught TypeError: can't access property "checked", document.getElementById(...) is null
    updateBulkActionsBar http://localhost:3000/settings/admin/disabled-entities:856
    applyFilters http://localhost:3000/settings/admin/disabled-entities:806
    <anonymous> http://localhost:3000/settings/admin/disabled-entities:828
    EventListener.handleEvent* http://localhost:3000/settings/admin/disabled-entities:810
  disabled-entities:856:15
  - Disabled entities in in the root settings but should be a sub setting of HAProxy plugin

---
