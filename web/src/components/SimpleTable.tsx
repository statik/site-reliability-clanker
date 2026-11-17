import { Fragment, useMemo, useState } from "react";
import type { ReactNode } from "react";
import {
  DataTable,
  Pagination,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableExpandedRow,
  TableExpandHeader,
  TableExpandRow,
  TableHead,
  TableHeader,
  TableRow,
  TableToolbar,
  TableToolbarContent,
  TableToolbarSearch,
} from "@carbon/react";
import { defined } from "@/lib/props";
import { ScrollRegion } from "@/components/common";

export interface Column<T> {
  key: string;
  header: string;
  /** The plain value, used to sort and to search. */
  value: (row: T) => string | number;
  /** The cell content, when it is more than the plain value. */
  render?: (row: T) => ReactNode;
}

interface Props<T> {
  title: string;
  description?: string;
  rows: T[];
  rowId: (row: T) => string;
  columns: Column<T>[];
  /** The content of an expanded row. Without it the rows do not expand. */
  expand?: (row: T) => ReactNode;
  /** Extra toolbar controls, for example filters. */
  toolbar?: ReactNode;
  /** A server-side search. Without it the table filters its own rows. */
  onSearch?: (text: string) => void;
  empty: string;
  pageSize?: number;
  size?: "xs" | "sm" | "md" | "lg";
}

type Cells = Record<string, string | number> & { id: string };

/** A sortable, searchable, paged Carbon data table over a list of records. */
export function SimpleTable<T>(props: Props<T>) {
  const { rows, rowId, columns, expand, pageSize = 25 } = props;
  const [page, setPage] = useState({ page: 1, pageSize });
  const byId = useMemo(() => new Map(rows.map((row) => [rowId(row), row])), [rows, rowId]);
  const tableRows = useMemo<Cells[]>(
    () =>
      rows.map((row) => {
        const cells: Cells = { id: rowId(row) };
        for (const column of columns) {
          cells[column.key] = column.value(row);
        }
        return cells;
      }),
    [rows, rowId, columns],
  );
  const headers = columns.map((column) => ({ key: column.key, header: column.header }));
  const renderers = new Map(columns.map((column) => [column.key, column.render]));
  const span = columns.length + (expand === undefined ? 0 : 1);

  return (
    <DataTable rows={tableRows} headers={headers} isSortable size={props.size ?? "md"}>
      {({ rows: shown, headers: heads, getHeaderProps, getRowProps, getTableProps,
        getToolbarProps, getTableContainerProps, getExpandedRowProps, getExpandHeaderProps,
        onInputChange }) => {
        const start = (page.page - 1) * page.pageSize;
        const visible = shown.slice(start, start + page.pageSize);
        return (
          <TableContainer title={props.title} description={props.description}
            {...getTableContainerProps()}>
            <TableToolbar {...defined(getToolbarProps())} aria-label={`${props.title} toolbar`}>
              <TableToolbarContent>
                <TableToolbarSearch persistent labelText={`Search ${props.title}`}
                  onChange={(event, value) => {
                    setPage((current) => ({ ...current, page: 1 }));
                    if (props.onSearch !== undefined) {
                      props.onSearch(value ?? "");
                    } else if (event !== "") {
                      onInputChange(event, value);
                    }
                  }} />
                {props.toolbar}
              </TableToolbarContent>
            </TableToolbar>
            <ScrollRegion label={props.title}>
              <Table {...getTableProps()} aria-label={props.title}>
                <TableHead>
                  <TableRow>
                    {expand !== undefined && <TableExpandHeader {...getExpandHeaderProps()} />}
                    {heads.map((header) => {
                      const { key, ...rest } = defined(getHeaderProps({ header }));
                      return <TableHeader key={key} {...rest}>{header.header}</TableHeader>;
                    })}
                  </TableRow>
                </TableHead>
                <TableBody>
                  {visible.length === 0 && (
                    <TableRow><TableCell colSpan={span}>{props.empty}</TableCell></TableRow>
                  )}
                  {visible.map((row) => {
                    const record = byId.get(row.id);
                    const cells = row.cells.map((cell) => {
                      const render = renderers.get(cell.info.header);
                      return (
                        <TableCell key={cell.id}>
                          {render !== undefined && record !== undefined
                            ? render(record) : String(cell.value ?? "")}
                        </TableCell>
                      );
                    });
                    if (expand === undefined) {
                      const { key, ...rest } = getRowProps({ row });
                      return <TableRow key={key} {...rest}>{cells}</TableRow>;
                    }
                    const { key, ...rest } = getRowProps({ row });
                    return (
                      <Fragment key={key}>
                        <TableExpandRow {...rest}>{cells}</TableExpandRow>
                        {row.isExpanded && record !== undefined && (
                          <TableExpandedRow colSpan={span} {...getExpandedRowProps({ row })}>
                            {expand(record)}
                          </TableExpandedRow>
                        )}
                      </Fragment>
                    );
                  })}
                </TableBody>
              </Table>
            </ScrollRegion>
            {shown.length > pageSize && (
              <Pagination page={page.page} pageSize={page.pageSize} totalItems={shown.length}
                pageSizes={[pageSize, pageSize * 2, pageSize * 4]}
                onChange={(next) => setPage({ page: next.page, pageSize: next.pageSize })} />
            )}
          </TableContainer>
        );
      }}
    </DataTable>
  );
}
