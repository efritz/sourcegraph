import React from 'react'

import BrainIcon from 'mdi-react/BrainIcon'

import { SettingsCascadeProps } from '@sourcegraph/shared/src/settings/settings'
import { Icon, Menu, MenuButton, MenuDivider, MenuHeader, MenuList, Position } from '@sourcegraph/wildcard'

import styles from './RepositoryMenu.module.scss'

export type RepositoryMenuContentProps = SettingsCascadeProps & {
    repoName: string
    revision: string
    filePath: string
}

export type RepositoryMenuProps = RepositoryMenuContentProps & {
    Content: typeof RepositoryMenuContent
}

export const RepositoryMenu: React.FunctionComponent<RepositoryMenuProps> = ({ Content, ...props }) => (
    <Menu className="btn-icon">
        <>
            <MenuButton className="text-decoration-none">
                <Icon as={BrainIcon} />
            </MenuButton>

            <MenuList position={Position.bottomEnd} className={styles.dropdownMenu}>
                <MenuHeader>Code intelligence</MenuHeader>
                <MenuDivider />
                <Content {...props} />
            </MenuList>
        </>
    </Menu>
)

export const RepositoryMenuContent: React.FunctionComponent<RepositoryMenuContentProps> = React.memo(() => (
    <div className="px-2 py-1">
        <h2>Unimplemented</h2>

        <p className="text-muted">Unimplemented (OSS version).</p>
    </div>
))
