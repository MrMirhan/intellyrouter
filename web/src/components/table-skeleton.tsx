import { Skeleton } from "@/components/ui/skeleton"
import { TableCell, TableRow } from "@/components/ui/table"

export function TableSkeleton({ columns, rows = 5 }: { columns: number; rows?: number }) {
  return Array.from({ length: rows }, (_, row) => (
    <TableRow key={row}>
      {Array.from({ length: columns }, (_, column) => (
        <TableCell key={column}>
          <Skeleton className="h-4 w-full max-w-32" />
        </TableCell>
      ))}
    </TableRow>
  ))
}
