/* SSE helper for HTMX */
/* This file provides SSE support that can be used alongside HTMX */

(function() {
  'use strict';

  // SSE connection manager
  window.SSE = {
    connections: {},

    connect: function(url, options) {
      options = options || {};

      if (this.connections[url]) {
        return this.connections[url];
      }

      const eventSource = new EventSource(url);
      this.connections[url] = eventSource;

      eventSource.onopen = function() {
        console.log('SSE connected:', url);
        if (options.onOpen) options.onOpen();
      };

      eventSource.onerror = function(e) {
        console.error('SSE error:', url, e);
        if (options.onError) options.onError(e);
      };

      return eventSource;
    },

    disconnect: function(url) {
      if (this.connections[url]) {
        this.connections[url].close();
        delete this.connections[url];
        console.log('SSE disconnected:', url);
      }
    },

    disconnectAll: function() {
      Object.keys(this.connections).forEach(function(url) {
        this.disconnect(url);
      }.bind(this));
    }
  };

  // Auto-cleanup on page unload
  window.addEventListener('beforeunload', function() {
    window.SSE.disconnectAll();
  });
})();
