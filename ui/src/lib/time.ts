// "12 min ago", "3 h ago", "2 d ago" from an RFC 3339 timestamp returned by the API.
export function timeAgo(iso: string, now = Date.now()): string {
	const s = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000))
	if (Number.isNaN(s)) return ''
	if (s < 60) return 'just now'
	if (s < 3600) return `${Math.floor(s / 60)} min ago`
	if (s < 86400) return `${Math.floor(s / 3600)} h ago`
	return `${Math.floor(s / 86400)} d ago`
}
