(() => {
  "use strict";

  let phonebook = document.getElementById("phonebook-live");
  if (!phonebook) return;

  const liveURL = phonebook.dataset.partyLiveUrl;
  if (!liveURL || !liveURL.startsWith("/parties/") || !liveURL.endsWith("/live")) return;

  const refresh = async () => {
    if (document.hidden || !phonebook.isConnected) return;
    if (phonebook.contains(document.activeElement) || phonebook.querySelector("details[open]")) return;

    try {
      const response = await fetch(liveURL, {
        method: "GET",
        credentials: "same-origin",
        cache: "no-store",
        redirect: "error",
        headers: { Accept: "text/html" },
      });
      if (!response.ok || !response.headers.get("content-type")?.startsWith("text/html")) return;

      const page = new DOMParser().parseFromString(await response.text(), "text/html");
      const nextPhonebook = page.getElementById("phonebook-live");
      if (!nextPhonebook || nextPhonebook.dataset.partyLiveUrl !== liveURL) return;
      if (nextPhonebook.innerHTML === phonebook.innerHTML) return;

      phonebook.replaceWith(nextPhonebook);
      phonebook = document.getElementById("phonebook-live");
    } catch (_) {
      // The current phonebook remains useful through brief network or AMI outages.
    }
  };

  window.setInterval(refresh, 3000);
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) refresh();
  });
})();
