// Copyright (c) Privasys. All rights reserved.
// Licensed under the GNU Affero General Public License v3.0. See LICENSE.

'use strict';

// publicFetch is the only way this page calls a public route.
//
// credentials: 'omit' means the browser attaches no cookie, and Accept is
// the only header, so nothing that could identify the visitor crosses the
// gateway. The runtime would strip a credential anyway and count it as a
// mistake, but a credential the browser sent has already been read by the
// gateway on the way in. Keep public calls this bare, always.
function publicFetch(url) {
  return fetch(url, { credentials: 'omit', headers: { Accept: 'application/json' } });
}

// show renders a response as the page's raw view: status line, the headers
// the runtime exposes, and the body.
async function show(outputId, responsePromise) {
  const out = document.getElementById(outputId);
  out.textContent = '...';
  try {
    const res = await responsePromise;
    const transport = res.headers.get('X-Privasys-Transport');
    const text = await res.text();
    out.textContent =
      'HTTP ' + res.status + (res.statusText ? ' ' + res.statusText : '') + '\n' +
      (transport ? 'X-Privasys-Transport: ' + transport + '\n' : '') +
      '\n' + text;
  } catch (err) {
    out.textContent = 'request failed: ' + err;
  }
}

document.getElementById('btn-status').addEventListener('click', () => {
  show('out-status', publicFetch('/api/v1/public/status'));
});

// The two private routes are fetched in the clear on purpose, to make the
// runtime's refusal visible. This is the call a real page must NOT make:
// a signed-in page sends private requests through the auth SDK's sealed
// session, never as a plain fetch.
document.getElementById('btn-me').addEventListener('click', () => {
  show('out-me', fetch('/api/v1/me', { headers: { Accept: 'application/json' } }));
});

document.getElementById('btn-events').addEventListener('click', () => {
  show('out-events', fetch('/api/v1/events', { headers: { Accept: 'application/json' } }));
});
