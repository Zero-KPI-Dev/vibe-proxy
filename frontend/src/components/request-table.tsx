import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Badge } from "@/components/ui/badge"
import type { RequestEvent } from "@/lib/types"

interface RequestTableProps {
  events: RequestEvent[]
}

function statusBadge(status: number) {
  if (status >= 200 && status < 300) return <Badge variant="success">{status}</Badge>
  if (status >= 400 && status < 500) return <Badge variant="warning">{status}</Badge>
  if (status >= 500) return <Badge variant="destructive">{status}</Badge>
  return <Badge variant="outline">{status}</Badge>
}

export function RequestTable({ events }: RequestTableProps) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Client</TableHead>
          <TableHead>Model</TableHead>
          <TableHead>Provider</TableHead>
          <TableHead>Status</TableHead>
          <TableHead className="text-right">Tokens</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {events.map((e) => (
          <TableRow key={e.request_id}>
            <TableCell className="font-medium">{e.client_name}</TableCell>
            <TableCell className="font-mono text-xs">{e.virtual_model}</TableCell>
            <TableCell className="font-mono text-xs">{e.channel_id}</TableCell>
            <TableCell>{statusBadge(e.status_code)}</TableCell>
            <TableCell className="text-right tabular-nums">
              {e.usage?.total_tokens ?? 0}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
