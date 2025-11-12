export default async function(req) {
  const url = new URL(req.url);
  const path = url.pathname;

  // Determine version from path
  let version = 'latest';
  if (path.startsWith('/v/')) {
    const match = path.match(/^\/v\/([^\/]+)/);
    if (match) version = match[1];
  }

  // For HTML navigation (no file extension), serve index.html
  if (!path.match(/\.[a-z]+$/i)) {
    const htmlUrl = `https://${url.host}/v/${version}/index.html`;
    const response = await fetch(htmlUrl);
    return new Response(response.body, {
      headers: {
        'Content-Type': 'text/html',
        'Cache-Control': 'no-cache'
      }
    });
  }

  // For assets, pass through
  const assetUrl = `https://${url.host}${path}`;
  return fetch(assetUrl);
}

