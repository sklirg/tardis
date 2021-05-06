TARDIS
======

Custom chat bot for my discord server.

Commands
--------

!hots <hero>
Aliases: !aram

Gets the ARAM builds for a hero in Heroes of the Storm. Courtesy of Thedude.

Development
-----------

Required environment variables:

.. list-table:: Environment variables

  * - Variable
    - Description
    - Required
    - Default
  * - TARDIS_DISCORD_TOKEN
    - Token for connecting to the Discord API Gateway
    - Yes
    - \-
  * - DATABASE_URL
    - Database URL for connecting to the database used for reaction roles
    - No
    - \-
