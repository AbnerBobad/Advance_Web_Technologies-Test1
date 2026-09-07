// data-service.js is the only module that talks to the ImageLab API.
// Week 3 adds the short-polling status requests here.
export async function submitImage(file) {
  const formData = new FormData();
  formData.append("file", file);

  const response = await fetch("/v1/images", { method: "POST", body: formData });

  let body = null;
  try {
    body = await response.json();
  } catch (_) {
    body = null;
  }

  return { accepted: response.status === 202, status: response.status, body };
}