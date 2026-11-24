'use strict';

// The app keeps no state of its own: every screen fetches from the CoolHome
// server, so a stale cache can never be shown as if it were live.
App({
  onLaunch() {
    console.log('coolhome: launch');
  },
  onError(err) {
    // Log the message only. An unhandled runtime error can carry the whole
    // request options object, and the access token travels in its headers.
    var message = err && err.message ? err.message : String(err);
    console.error('coolhome: unhandled error: ' + message);
  },
});
